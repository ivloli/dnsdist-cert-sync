package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Nacos   NacosConfig   `yaml:"nacos"`
	Cert    CertConfig    `yaml:"cert"`
	DNSDist DNSDistConfig `yaml:"dnsdist"`
	Sync    SyncConfig    `yaml:"sync"`
}

type NacosConfig struct {
	Addr      string `yaml:"addr"`
	Namespace string `yaml:"namespace"`
	Group     string `yaml:"group"`
	DataID    string `yaml:"data_id"`
	Username  string `yaml:"username"`
	Password  string `yaml:"password"`
}

type CertConfig struct {
	CertFile    string `yaml:"cert_file"`
	KeyFile     string `yaml:"key_file"`
	ChainFile   string `yaml:"chain_file"`
	RawDumpFile string `yaml:"raw_dump_file"`
	Owner       string `yaml:"owner"`
	Group       string `yaml:"group"`
	CertMode    string `yaml:"cert_mode"`
	KeyMode     string `yaml:"key_mode"`
	ChainMode   string `yaml:"chain_mode"`
	RawDumpMode string `yaml:"raw_dump_mode"`
}

type DNSDistConfig struct {
	BinaryPath       string `yaml:"binary_path"`
	ControlAddr      string `yaml:"control_addr"`
	ControlKey       string `yaml:"control_key"`
	ReloadLuaCommand string `yaml:"reload_lua_command"`
	ReloadCommand    string `yaml:"reload_command"`
}

type SyncConfig struct {
	PollInterval time.Duration `yaml:"poll_interval"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	expanded := os.ExpandEnv(string(data))
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Nacos.Addr == "" {
		return fmt.Errorf("nacos.addr is required")
	}
	if c.Nacos.Group == "" {
		c.Nacos.Group = "certs"
	}
	if c.Nacos.DataID == "" {
		return fmt.Errorf("nacos.data_id is required")
	}

	if c.Cert.CertFile == "" {
		return fmt.Errorf("cert.cert_file is required")
	}
	if c.Cert.KeyFile == "" {
		return fmt.Errorf("cert.key_file is required")
	}
	if c.Cert.Owner == "" {
		c.Cert.Owner = "_dnsdist"
	}
	if c.Cert.Group == "" {
		c.Cert.Group = "_dnsdist"
	}
	if c.Cert.CertMode == "" {
		c.Cert.CertMode = "0755"
	}
	if c.Cert.KeyMode == "" {
		c.Cert.KeyMode = "0755"
	}
	if c.Cert.ChainMode == "" {
		c.Cert.ChainMode = "0644"
	}
	if c.Cert.RawDumpMode == "" {
		c.Cert.RawDumpMode = "0640"
	}

	if c.DNSDist.BinaryPath == "" {
		c.DNSDist.BinaryPath = "/usr/bin/dnsdist"
	}
	if c.DNSDist.ReloadLuaCommand == "" {
		c.DNSDist.ReloadLuaCommand = "reloadAllCertificates()"
	}
	if c.Sync.PollInterval <= 0 {
		c.Sync.PollInterval = 30 * time.Second
	}

	if c.DNSDist.ReloadCommand == "" {
		if c.DNSDist.ControlAddr == "" {
			return fmt.Errorf("dnsdist.control_addr is required when dnsdist.reload_command is empty")
		}
		if strings.TrimSpace(c.DNSDist.ControlKey) == "" {
			return fmt.Errorf("dnsdist.control_key is required when dnsdist.reload_command is empty")
		}
	}

	return nil
}
