package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

var version = "dev"

type agentYAML struct {
	Addr    string `yaml:"addr"`
	Token   string `yaml:"token"`
	TLSCert string `yaml:"tls_cert"`
	TLSKey  string `yaml:"tls_key"`
}

func main() {
	configPath := flag.String("config", "", "YAML config path (default: <executable-dir>/bedrock-agent.yaml)")
	addrFlag := flag.String("addr", "", "agent listen address")
	tokenFlag := flag.String("token", "", "agent bearer token")
	certFile := flag.String("tls-cert", "", "TLS certificate path")
	keyFile := flag.String("tls-key", "", "TLS private key path")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		os.Exit(0)
	}

	cfgPath := strings.TrimSpace(*configPath)
	if cfgPath == "" {
		cfgPath = defaultConfigPath()
	}
	fileCfg := loadAgentConfigFile(cfgPath)

	addr := pick(*addrFlag, os.Getenv("BEDROCK_AGENT_ADDR"), fileCfg.Addr, ":9091")
	token := pick(*tokenFlag, os.Getenv("BEDROCK_AGENT_TOKEN"), fileCfg.Token, "")
	cert := pick(*certFile, os.Getenv("BEDROCK_AGENT_TLS_CERT"), fileCfg.TLSCert, "")
	key := pick(*keyFile, os.Getenv("BEDROCK_AGENT_TLS_KEY"), fileCfg.TLSKey, "")

	if strings.TrimSpace(token) == "" {
		fmt.Fprintln(os.Stderr, "BEDROCK_AGENT_TOKEN, -token, or token in bedrock-agent.yaml is required")
		os.Exit(1)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", withAuth(token, healthzHandler))
	mux.HandleFunc("/upload", withAuth(token, uploadHandler))
	mux.HandleFunc("/exec", withAuth(token, execHandler))

	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      5 * time.Minute,
	}

	if cert != "" && key != "" {
		if err := server.ListenAndServeTLS(cert, key); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintf(os.Stderr, "agent server failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "agent server failed: %v\n", err)
		os.Exit(1)
	}
}

func defaultConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return filepath.Join(filepath.Dir(exe), "bedrock-agent.yaml")
}

func loadAgentConfigFile(path string) agentYAML {
	if path == "" {
		return agentYAML{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return agentYAML{}
		}
		fmt.Fprintf(os.Stderr, "read config %s: %v\n", path, err)
		os.Exit(1)
	}
	var cfg agentYAML
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		fmt.Fprintf(os.Stderr, "invalid YAML in %s: %v\n", path, err)
		os.Exit(1)
	}
	return cfg
}

func pick(first string, rest ...string) string {
	if strings.TrimSpace(first) != "" {
		return strings.TrimSpace(first)
	}
	for _, r := range rest {
		if strings.TrimSpace(r) != "" {
			return strings.TrimSpace(r)
		}
	}
	return ""
}

func withAuth(token string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
		if authHeader != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fmt.Fprintf(w, "ok (%s %s)", runtime.GOOS, version)
}

func uploadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	targetPath := strings.TrimSpace(r.Header.Get("X-Target-Path"))
	if targetPath == "" {
		http.Error(w, "missing X-Target-Path header", http.StatusBadRequest)
		return
	}
	archiveFormat := normalizeArchiveFormat(r.Header.Get("X-Archive-Format"))

	if err := os.MkdirAll(targetPath, 0755); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := extractArchive(r.Body, targetPath, archiveFormat); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "uploaded to %s", targetPath)
}

func execHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Script  string `json:"script"`
		WorkDir string `json:"work_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Script) == "" {
		http.Error(w, "script is required", http.StatusBadRequest)
		return
	}

	cmd := commandForCurrentOS(req.Script)
	if strings.TrimSpace(req.WorkDir) != "" {
		cmd.Dir = req.WorkDir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		if len(output) > 0 {
			http.Error(w, string(output), http.StatusInternalServerError)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Write(output)
}

func extractArchive(src io.Reader, targetDir, format string) error {
	tmpFile, err := os.CreateTemp("", "bedrock-upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, src); err != nil {
		return err
	}
	if _, err := tmpFile.Seek(0, 0); err != nil {
		return err
	}

	if normalizeArchiveFormat(format) == "zip" {
		return extractZip(tmpFile.Name(), targetDir)
	}
	return extractTarGz(tmpFile, targetDir)
}

func extractTarGz(src io.Reader, targetDir string) error {
	gzipReader, err := gzip.NewReader(src)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		targetPath := filepath.Join(targetDir, filepath.Clean(header.Name))
		relPath, err := filepath.Rel(targetDir, targetPath)
		if err != nil {
			return err
		}
		if strings.HasPrefix(relPath, "..") {
			return fmt.Errorf("illegal archive path: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(targetPath, os.FileMode(header.Mode)); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tarReader); err != nil {
				file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
	}
}

func extractZip(srcPath, targetDir string) error {
	archive, err := zip.OpenReader(srcPath)
	if err != nil {
		return err
	}
	defer archive.Close()

	for _, file := range archive.File {
		targetPath := filepath.Join(targetDir, filepath.Clean(file.Name))
		relPath, err := filepath.Rel(targetDir, targetPath)
		if err != nil {
			return err
		}
		if strings.HasPrefix(relPath, "..") {
			return fmt.Errorf("illegal archive path: %s", file.Name)
		}

		if file.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, file.Mode()); err != nil {
				return err
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
			return err
		}

		reader, err := file.Open()
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(targetPath, os.O_CREATE|os.O_RDWR|os.O_TRUNC, file.Mode())
		if err != nil {
			reader.Close()
			return err
		}
		if _, err := io.Copy(dst, reader); err != nil {
			dst.Close()
			reader.Close()
			return err
		}
		if err := dst.Close(); err != nil {
			reader.Close()
			return err
		}
		if err := reader.Close(); err != nil {
			return err
		}
	}

	return nil
}

func commandForCurrentOS(script string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	}
	return exec.Command("sh", "-lc", script)
}

func normalizeArchiveFormat(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "zip":
		return "zip"
	default:
		return "gzip"
	}
}
