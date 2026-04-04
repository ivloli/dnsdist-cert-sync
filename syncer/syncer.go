package syncer

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/clients/config_client"
	"github.com/nacos-group/nacos-sdk-go/v2/vo"

	"coredns-dev/dnsdist-cert-sync/config"
)

type Syncer struct {
	cfg   *config.Config
	cli   config_client.IConfigClient
	mu    sync.Mutex
	lastH string
}

type certPayload struct {
	Cert      string
	Key       string
	CA        string
	FullChain string
}

func New(cfg *config.Config, nacosClient config_client.IConfigClient) *Syncer {
	return &Syncer{cfg: cfg, cli: nacosClient}
}

func (s *Syncer) Start(ctx context.Context) error {
	if err := s.syncFromNacos("startup"); err != nil {
		log.Printf("[cert-sync] startup sync failed: %v", err)
	}

	err := s.cli.ListenConfig(vo.ConfigParam{
		DataId: s.cfg.Nacos.DataID,
		Group:  s.cfg.Nacos.Group,
		OnChange: func(_, _, _, data string) {
			if err := s.applyContent(data, "listen"); err != nil {
				log.Printf("[cert-sync] listen apply failed: %v", err)
			}
		},
	})
	if err != nil {
		return fmt.Errorf("register nacos listener: %w", err)
	}

	ticker := time.NewTicker(s.cfg.Sync.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := s.syncFromNacos("poll"); err != nil {
				log.Printf("[cert-sync] poll sync failed: %v", err)
			}
		}
	}
}

func (s *Syncer) syncFromNacos(source string) error {
	content, err := s.cli.GetConfig(vo.ConfigParam{DataId: s.cfg.Nacos.DataID, Group: s.cfg.Nacos.Group})
	if err != nil {
		return fmt.Errorf("get nacos config: %w", err)
	}
	return s.applyContent(content, source)
}

func (s *Syncer) applyContent(content, source string) error {
	content = strings.TrimSpace(content)
	if content == "" {
		return fmt.Errorf("nacos content is empty")
	}

	h := sha256.Sum256([]byte(content))
	hash := hex.EncodeToString(h[:])

	s.mu.Lock()
	if hash == s.lastH {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	p, err := parsePayload(content)
	if err != nil {
		return fmt.Errorf("parse cert payload: %w", err)
	}

	p.Cert = normalizePEM(p.Cert)
	p.Key = normalizePEM(p.Key)
	p.CA = normalizePEM(p.CA)
	p.FullChain = normalizePEM(p.FullChain)

	if err := validateCertPair(p.Cert, p.Key); err != nil {
		return fmt.Errorf("validate cert/key: %w", err)
	}

	oldFp := certFingerprintFromFile(s.cfg.Cert.CertFile)
	newFp := certFingerprint([]byte(p.Cert))

	if err := s.writeFiles(content, p); err != nil {
		return err
	}

	if err := s.reloadDNSDist(); err != nil {
		return fmt.Errorf("reload dnsdist failed: %w", err)
	}

	s.mu.Lock()
	s.lastH = hash
	s.mu.Unlock()

	log.Printf("[cert-sync] applied from %s: cert fingerprint %s -> %s", source, oldFp, newFp)
	return nil
}

func (s *Syncer) writeFiles(raw string, p certPayload) error {
	certMode, err := parseFileMode(s.cfg.Cert.CertMode)
	if err != nil {
		return fmt.Errorf("parse cert.cert_mode: %w", err)
	}
	keyMode, err := parseFileMode(s.cfg.Cert.KeyMode)
	if err != nil {
		return fmt.Errorf("parse cert.key_mode: %w", err)
	}
	chainMode, err := parseFileMode(s.cfg.Cert.ChainMode)
	if err != nil {
		return fmt.Errorf("parse cert.chain_mode: %w", err)
	}
	rawDumpMode, err := parseFileMode(s.cfg.Cert.RawDumpMode)
	if err != nil {
		return fmt.Errorf("parse cert.raw_dump_mode: %w", err)
	}

	if s.cfg.Cert.RawDumpFile != "" {
		if err := atomicWrite(s.cfg.Cert.RawDumpFile, []byte(raw), rawDumpMode); err != nil {
			return fmt.Errorf("write raw_dump_file: %w", err)
		}
		if err := applyOwnerAndMode(s.cfg.Cert.RawDumpFile, s.cfg.Cert.Owner, s.cfg.Cert.Group, rawDumpMode); err != nil {
			return fmt.Errorf("set raw_dump_file owner/mode: %w", err)
		}
	}

	certContent := p.FullChain
	if certContent == "" {
		certContent = p.Cert
		if p.CA != "" {
			certContent = certContent + "\n" + p.CA
		}
	}
	if err := atomicWrite(s.cfg.Cert.CertFile, []byte(certContent), certMode); err != nil {
		return fmt.Errorf("write cert_file: %w", err)
	}
	if err := applyOwnerAndMode(s.cfg.Cert.CertFile, s.cfg.Cert.Owner, s.cfg.Cert.Group, certMode); err != nil {
		return fmt.Errorf("set cert_file owner/mode: %w", err)
	}
	if err := atomicWrite(s.cfg.Cert.KeyFile, []byte(p.Key), keyMode); err != nil {
		return fmt.Errorf("write key_file: %w", err)
	}
	if err := applyOwnerAndMode(s.cfg.Cert.KeyFile, s.cfg.Cert.Owner, s.cfg.Cert.Group, keyMode); err != nil {
		return fmt.Errorf("set key_file owner/mode: %w", err)
	}
	if s.cfg.Cert.ChainFile != "" {
		if err := atomicWrite(s.cfg.Cert.ChainFile, []byte(p.CA), chainMode); err != nil {
			return fmt.Errorf("write chain_file: %w", err)
		}
		if err := applyOwnerAndMode(s.cfg.Cert.ChainFile, s.cfg.Cert.Owner, s.cfg.Cert.Group, chainMode); err != nil {
			return fmt.Errorf("set chain_file owner/mode: %w", err)
		}
	}
	return nil
}

func parseFileMode(v string) (os.FileMode, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0, fmt.Errorf("empty mode")
	}
	n, err := strconv.ParseUint(v, 8, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid mode %q", v)
	}
	return os.FileMode(n), nil
}

func applyOwnerAndMode(path, ownerName, groupName string, mode os.FileMode) error {
	uid, err := resolveUID(ownerName)
	if err != nil {
		return err
	}
	gid, err := resolveGID(groupName)
	if err != nil {
		return err
	}
	if err := os.Chown(path, uid, gid); err != nil {
		return err
	}
	if err := os.Chmod(path, mode); err != nil {
		return err
	}
	return nil
}

func resolveUID(v string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return -1, fmt.Errorf("empty owner")
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n, nil
	}
	u, err := user.Lookup(v)
	if err != nil {
		return -1, fmt.Errorf("lookup owner %q: %w", v, err)
	}
	n, err := strconv.Atoi(u.Uid)
	if err != nil {
		return -1, fmt.Errorf("parse owner uid %q: %w", u.Uid, err)
	}
	return n, nil
}

func resolveGID(v string) (int, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return -1, fmt.Errorf("empty group")
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n, nil
	}
	g, err := user.LookupGroup(v)
	if err != nil {
		return -1, fmt.Errorf("lookup group %q: %w", v, err)
	}
	n, err := strconv.Atoi(g.Gid)
	if err != nil {
		return -1, fmt.Errorf("parse group gid %q: %w", g.Gid, err)
	}
	return n, nil
}

func (s *Syncer) reloadDNSDist() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if strings.TrimSpace(s.cfg.DNSDist.ReloadCommand) != "" {
		cmd = exec.CommandContext(ctx, "sh", "-c", s.cfg.DNSDist.ReloadCommand)
	} else {
		cmd = exec.CommandContext(
			ctx,
			s.cfg.DNSDist.BinaryPath,
			"-c", s.cfg.DNSDist.ControlAddr,
			"-k", s.cfg.DNSDist.ControlKey,
			"-e", s.cfg.DNSDist.ReloadLuaCommand,
		)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("command=%q output=%s err=%w", strings.Join(cmd.Args, " "), strings.TrimSpace(string(out)), err)
	}
	if s := strings.TrimSpace(string(out)); s != "" {
		log.Printf("[cert-sync] reload output: %s", s)
	}
	return nil
}

func parsePayload(content string) (certPayload, error) {
	var root any
	if err := json.Unmarshal([]byte(content), &root); err != nil {
		return certPayload{}, err
	}

	cert, ok := findString(root, map[string]struct{}{
		"cert": {}, "certificate": {}, "crt": {}, "tlscert": {}, "tlscrt": {}, "fullchain": {}, "certpem": {}, "certificate_pem": {},
	})
	if !ok {
		return certPayload{}, fmt.Errorf("cert field not found")
	}
	key, ok := findString(root, map[string]struct{}{
		"key": {}, "privatekey": {}, "private_key": {}, "tlskey": {}, "keypem": {}, "private_key_pem": {},
	})
	if !ok {
		return certPayload{}, fmt.Errorf("key field not found")
	}
	ca, _ := findString(root, map[string]struct{}{
		"ca": {}, "chain": {}, "cabundle": {}, "ca_bundle": {}, "bundle": {},
	})
	fullChain, _ := findString(root, map[string]struct{}{
		"certificate_fullchain_pem": {}, "fullchain_pem": {}, "fullchain": {},
	})

	return certPayload{Cert: cert, Key: key, CA: ca, FullChain: fullChain}, nil
}

func findString(v any, keys map[string]struct{}) (string, bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			if _, ok := keys[strings.ToLower(strings.TrimSpace(k))]; ok {
				if s, ok := val.(string); ok && strings.TrimSpace(s) != "" {
					return s, true
				}
			}
			if s, ok := findString(val, keys); ok {
				return s, true
			}
		}
	case []any:
		for _, item := range x {
			if s, ok := findString(item, keys); ok {
				return s, true
			}
		}
	}
	return "", false
}

func normalizePEM(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if !strings.Contains(s, "-----BEGIN") {
		if decoded, err := base64.StdEncoding.DecodeString(s); err == nil {
			t := strings.TrimSpace(string(decoded))
			if strings.Contains(t, "-----BEGIN") {
				s = t
			}
		}
	}
	s = strings.ReplaceAll(s, "\\n", "\n")
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s
}

func validateCertPair(certPEM, keyPEM string) error {
	pair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return err
	}
	if len(pair.Certificate) == 0 {
		return fmt.Errorf("empty certificate chain")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return err
	}
	now := time.Now()
	if now.Before(leaf.NotBefore) {
		return fmt.Errorf("certificate not valid before %s", leaf.NotBefore.Format(time.RFC3339))
	}
	if now.After(leaf.NotAfter) {
		return fmt.Errorf("certificate expired at %s", leaf.NotAfter.Format(time.RFC3339))
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func certFingerprintFromFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "none"
	}
	return certFingerprint(data)
}

func certFingerprint(certPEM []byte) string {
	for {
		var block *pem.Block
		block, certPEM = pem.Decode(certPEM)
		if block == nil {
			break
		}
		if block.Type == "CERTIFICATE" {
			h := sha256.Sum256(block.Bytes)
			return hex.EncodeToString(h[:8])
		}
	}
	return "unknown"
}
