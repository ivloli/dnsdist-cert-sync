package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/nacos-group/nacos-sdk-go/v2/clients"
	"github.com/nacos-group/nacos-sdk-go/v2/common/constant"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"

	"coredns-dev/dnsdist-cert-sync/config"
	"coredns-dev/dnsdist-cert-sync/syncer"
)

func main() {
	cfgPath := flag.String("config", "/etc/dnsdist-cert-sync/config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	host, port := splitHostPort(cfg.Nacos.Addr)
	serverConfigs := []constant.ServerConfig{
		*constant.NewServerConfig(host, port),
	}
	clientConfig := *constant.NewClientConfig(
		constant.WithNamespaceId(cfg.Nacos.Namespace),
		constant.WithTimeoutMs(5000),
		constant.WithNotLoadCacheAtStart(true),
		constant.WithLogDir("/tmp/nacos/log"),
		constant.WithCacheDir("/tmp/nacos/cache"),
		constant.WithLogLevel("warn"),
		constant.WithUsername(cfg.Nacos.Username),
		constant.WithPassword(cfg.Nacos.Password),
	)

	nacosClient, err := clients.NewConfigClient(vo.NacosClientParam{
		ClientConfig:  &clientConfig,
		ServerConfigs: serverConfigs,
	})
	if err != nil {
		log.Fatalf("init nacos client: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("dnsdist-cert-sync starting (nacos=%s ns=%q group=%s data_id=%s poll=%s)", cfg.Nacos.Addr, cfg.Nacos.Namespace, cfg.Nacos.Group, cfg.Nacos.DataID, cfg.Sync.PollInterval)
	s := syncer.New(cfg, nacosClient)
	if err := s.Start(ctx); err != nil {
		log.Printf("dnsdist-cert-sync stopped with error: %v", err)
		os.Exit(1)
	}
	log.Printf("dnsdist-cert-sync stopped")
}

func splitHostPort(addr string) (host string, port uint64) {
	port = 8848
	idx := strings.LastIndex(addr, ":")
	if idx < 0 {
		return addr, port
	}
	host = addr[:idx]
	if p, err := strconv.ParseUint(addr[idx+1:], 10, 64); err == nil {
		port = p
	}
	return
}
