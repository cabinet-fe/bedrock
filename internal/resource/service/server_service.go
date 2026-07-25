package service

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"

	"bedrock/internal/deployer"
	"bedrock/internal/pkg"
	"bedrock/internal/resource/model"
	"bedrock/internal/resource/repository"
)

type ServerService struct {
	repo  *repository.ServerRepository
	creds *CredentialService
}

func NewServerService(repo *repository.ServerRepository, creds *CredentialService) *ServerService {
	return &ServerService{repo: repo, creds: creds}
}

type CreateServerInput struct {
	Name              string `json:"name"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	OSType            string `json:"os_type"`
	Username          string `json:"username"`
	AuthType          string `json:"auth_type"`
	CredentialID      *uint  `json:"credential_id"`
	AgentURL          string `json:"agent_url"`
	AgentCredentialID *uint  `json:"agent_credential_id"`
	Description       string `json:"description"`
	Tags              string `json:"tags"`
}

type UpdateServerInput struct {
	Name                 *string `json:"name"`
	Host                 *string `json:"host"`
	Port                 *int    `json:"port"`
	OSType               *string `json:"os_type"`
	Username             *string `json:"username"`
	AuthType             *string `json:"auth_type"`
	CredentialID         *uint   `json:"credential_id"`
	ClearCredential      bool    `json:"clear_credential"`
	AgentURL             *string `json:"agent_url"`
	AgentCredentialID    *uint   `json:"agent_credential_id"`
	ClearAgentCredential bool    `json:"clear_agent_credential"`
	Description          *string `json:"description"`
	Tags                 *string `json:"tags"`
}

func (s *ServerService) Create(createdBy uint, in CreateServerInput, canUseCredential bool) (*model.Server, error) {
	srv, err := s.buildServer(0, in, canUseCredential)
	if err != nil {
		return nil, err
	}
	srv.CreatedBy = createdBy
	srv.Status = "unknown"
	if err := s.repo.Create(srv); err != nil {
		return nil, err
	}
	return srv, nil
}

func (s *ServerService) buildServer(id uint, in CreateServerInput, canUseCredential bool) (*model.Server, error) {
	name := strings.TrimSpace(in.Name)
	host := strings.TrimSpace(in.Host)
	authType := normalizeServerAuth(in.AuthType)
	if name == "" {
		return nil, errorsNew("名称不能为空")
	}
	if authType != "agent" && host == "" {
		return nil, errorsNew("主机不能为空")
	}
	if (in.CredentialID != nil && *in.CredentialID > 0) || (in.AgentCredentialID != nil && *in.AgentCredentialID > 0) {
		if !canUseCredential {
			return nil, NewForbidden("绑定凭证需要 resource_credentials:use 权限")
		}
	}
	if in.CredentialID != nil && *in.CredentialID > 0 {
		if _, err := s.creds.Get(*in.CredentialID); err != nil {
			return nil, errorsNew("凭证不存在")
		}
	}
	if in.AgentCredentialID != nil && *in.AgentCredentialID > 0 {
		if _, err := s.creds.Get(*in.AgentCredentialID); err != nil {
			return nil, errorsNew("Agent 凭证不存在")
		}
	}
	port := in.Port
	if port <= 0 {
		port = 22
	}
	osType := strings.ToLower(strings.TrimSpace(in.OSType))
	if osType != "windows" {
		osType = "linux"
	}
	return &model.Server{
		ID:                id,
		Name:              name,
		Host:              host,
		Port:              port,
		OSType:            osType,
		Username:          strings.TrimSpace(in.Username),
		AuthType:          authType,
		CredentialID:      nilIfZero(in.CredentialID),
		AgentURL:          strings.TrimSpace(in.AgentURL),
		AgentCredentialID: nilIfZero(in.AgentCredentialID),
		Description:       strings.TrimSpace(in.Description),
		Tags:              strings.TrimSpace(in.Tags),
	}, nil
}

func (s *ServerService) Update(id uint, in UpdateServerInput, canUseCredential bool) (*model.Server, error) {
	existing, err := s.repo.FindByID(id)
	if err != nil {
		return nil, NewNotFound("服务器不存在")
	}
	prevCred, prevAgent := existing.CredentialID, existing.AgentCredentialID
	if in.Name != nil {
		existing.Name = strings.TrimSpace(*in.Name)
	}
	if in.Host != nil {
		existing.Host = strings.TrimSpace(*in.Host)
	}
	if in.Port != nil && *in.Port > 0 {
		existing.Port = *in.Port
	}
	if in.OSType != nil {
		osType := strings.ToLower(strings.TrimSpace(*in.OSType))
		if osType != "windows" {
			osType = "linux"
		}
		existing.OSType = osType
	}
	if in.Username != nil {
		existing.Username = strings.TrimSpace(*in.Username)
	}
	if in.AuthType != nil {
		existing.AuthType = normalizeServerAuth(*in.AuthType)
	}
	if in.AgentURL != nil {
		existing.AgentURL = strings.TrimSpace(*in.AgentURL)
	}
	if in.Description != nil {
		existing.Description = strings.TrimSpace(*in.Description)
	}
	if in.Tags != nil {
		existing.Tags = strings.TrimSpace(*in.Tags)
	}
	if in.ClearCredential {
		existing.CredentialID = nil
	} else if in.CredentialID != nil {
		if !credentialIDEqual(prevCred, in.CredentialID) && !canUseCredential {
			return nil, NewForbidden("绑定/修改凭证需要 resource_credentials:use 权限")
		}
		if *in.CredentialID == 0 {
			existing.CredentialID = nil
		} else {
			if _, err := s.creds.Get(*in.CredentialID); err != nil {
				return nil, errorsNew("凭证不存在")
			}
			existing.CredentialID = in.CredentialID
		}
	}
	if in.ClearAgentCredential {
		existing.AgentCredentialID = nil
	} else if in.AgentCredentialID != nil {
		if !credentialIDEqual(prevAgent, in.AgentCredentialID) && !canUseCredential {
			return nil, NewForbidden("绑定/修改凭证需要 resource_credentials:use 权限")
		}
		if *in.AgentCredentialID == 0 {
			existing.AgentCredentialID = nil
		} else {
			if _, err := s.creds.Get(*in.AgentCredentialID); err != nil {
				return nil, errorsNew("Agent 凭证不存在")
			}
			existing.AgentCredentialID = in.AgentCredentialID
		}
	}
	if existing.Name == "" {
		return nil, errorsNew("名称不能为空")
	}
	if existing.AuthType != "agent" && existing.Host == "" {
		return nil, errorsNew("主机不能为空")
	}
	if err := s.repo.Update(existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *ServerService) Delete(id uint) error {
	if _, err := s.repo.FindByID(id); err != nil {
		return NewNotFound("服务器不存在")
	}
	n, err := s.repo.CountDeployTargets(id)
	if err != nil {
		return err
	}
	if n > 0 {
		return NewConflict("该服务器仍被部署目标引用，无法删除")
	}
	return s.repo.Delete(id)
}

func (s *ServerService) Get(id uint) (*model.Server, error) {
	srv, err := s.repo.FindByID(id)
	if err != nil {
		return nil, NewNotFound("服务器不存在")
	}
	return srv, nil
}

func (s *ServerService) List(q pkg.ListQuery, keyword, tag string) ([]model.Server, int64, error) {
	return s.repo.List(q, keyword, tag)
}

func (s *ServerService) TestConnection(id uint) (string, error) {
	srv, err := s.repo.FindByID(id)
	if err != nil {
		return "", NewNotFound("服务器不存在")
	}
	var output string
	if srv.AuthType == "agent" {
		output, err = s.testAgent(srv)
	} else {
		output, err = s.testSSH(srv)
	}
	if err != nil {
		_ = s.repo.UpdateStatus(id, "offline")
		return "", err
	}
	_ = s.repo.UpdateStatus(id, "online")
	return output, nil
}

func (s *ServerService) testSSH(srv *model.Server) (string, error) {
	password, privateKey := "", ""
	authType := srv.AuthType
	if srv.CredentialID != nil {
		cred, secret, passphrase, err := s.creds.GetDecrypted(*srv.CredentialID)
		if err != nil {
			return "", err
		}
		switch cred.Type {
		case "ssh_key":
			privateKey = secret
			authType = "key"
			if passphrase != "" {
				// deployer parsePrivateKey tries without passphrase; passphrase support limited — keep key as-is
				_ = passphrase
			}
		default:
			password = secret
			authType = "password"
		}
		if srv.Username == "" {
			srv.Username = cred.Username
		}
	}
	if authType == "ssh_agent" {
		authType = "key"
	}
	addr := fmt.Sprintf("%s:%d", srv.Host, srv.Port)
	if srv.Port == 0 {
		addr = srv.Host + ":22"
	}
	info := deployer.ServerInfo{
		Host:       srv.Host,
		Port:       srv.Port,
		OSType:     srv.OSType,
		Username:   srv.Username,
		AuthType:   authType,
		Password:   password,
		PrivateKey: privateKey,
	}
	authMethods, err := deployer.SSHAuthMethods(info)
	if err != nil {
		return "", fmt.Errorf("无法认证：%v", err)
	}
	config := &ssh.ClientConfig{
		User:            srv.Username,
		Auth:            authMethods,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error { return nil },
		Timeout:         10 * time.Second,
	}
	client, err := ssh.Dial("tcp", addr, config)
	if err != nil {
		return "", fmt.Errorf("连接失败: %w", err)
	}
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	cmd := "uname -a"
	if srv.OSType == "windows" {
		cmd = "cmd /c ver"
	}
	out, err := session.CombinedOutput(cmd)
	if err != nil {
		return "", fmt.Errorf("执行命令失败: %w", err)
	}
	return string(out), nil
}

func (s *ServerService) testAgent(srv *model.Server) (string, error) {
	agentURL := strings.TrimSpace(srv.AgentURL)
	if agentURL == "" {
		return "", errorsNew("agent_url 不能为空")
	}
	u, err := url.Parse(agentURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errorsNew("agent_url 无效")
	}
	healthURL := strings.TrimRight(agentURL, "/") + "/health"
	req, err := http.NewRequest(http.MethodGet, healthURL, nil)
	if err != nil {
		return "", err
	}
	if srv.AgentCredentialID != nil {
		_, token, _, err := s.creds.GetDecrypted(*srv.AgentCredentialID)
		if err != nil {
			return "", err
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("agent 连接失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("agent 返回 %d: %s", resp.StatusCode, string(body))
	}
	return string(body), nil
}

func normalizeServerAuth(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "key", "ssh_key":
		return "key"
	case "ssh_agent", "agent_ssh":
		return "ssh_agent"
	case "agent":
		return "agent"
	default:
		return "password"
	}
}

func nilIfZero(p *uint) *uint {
	if p == nil || *p == 0 {
		return nil
	}
	return p
}
