package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"crypto/x509"
	"embed"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/ssh"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed atlas_bootstrap.sh
var atlasBootstrapScript embed.FS

const (
	sshPort                = "22"
	maxBootstrapBundleSize = 32 << 20
	maxSSHPasswordSize     = 1024
	provisioningJobTimeout = 30 * time.Minute
	sshProbeTimeout        = 5 * time.Second
	sshConnectTimeout      = 12 * time.Second
	sshSudoPrompt          = "__YUNSHAO_SUDO_PASSWORD__"
	sshArchiveInputMarker  = "__YUNSHAO_ARCHIVE_INPUT__"
	sshCAInputMarker       = "__YUNSHAO_CA_INPUT__"
	sshTicketInputMarker   = "__YUNSHAO_TICKET_INPUT__"
	sshBootstrapDoneMarker = "__YUNSHAO_BOOTSTRAP_DONE__"
)

var linuxUsernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)
var sshFingerprintPattern = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)
var errProvisioningInProgress = errors.New("provisioning in progress")

type sshProvisionInput struct {
	Username           string `json:"ssh_username"`
	Password           string `json:"ssh_password"`
	HostKeyFingerprint string `json:"host_key_fingerprint"`
	DurationSeconds    *int64 `json:"duration_seconds"`
}

type provisioningCredentials struct {
	username    string
	password    []byte
	fingerprint string
}

type sshOutputWriter struct {
	chunks chan []byte
	mu     sync.Mutex
	closed bool
}

func (w *sshOutputWriter) Write(p []byte) (int, error) {
	copyOfChunk := append([]byte(nil), p...)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return len(p), nil
	}
	select {
	case w.chunks <- copyOfChunk:
	default:
		// Remote build output is suppressed in the fixed bootstrap script. If
		// a tool still emits a burst, retain recent output markers only.
		select {
		case <-w.chunks:
		default:
		}
		select {
		case w.chunks <- copyOfChunk:
		default:
		}
	}
	return len(p), nil
}

func (w *sshOutputWriter) close() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
}

type sshMarkerReader struct {
	chunks  <-chan []byte
	done    <-chan struct{}
	pending []byte
}

func (r *sshMarkerReader) readUntil(ctx context.Context, marker string) ([]byte, error) {
	for {
		needle := []byte(marker)
		if index := bytes.Index(r.pending, needle); index >= 0 {
			value := append([]byte(nil), r.pending[:index]...)
			r.pending = append([]byte(nil), r.pending[index+len(needle):]...)
			return value, nil
		}
		if len(r.pending) > 16<<10 {
			return nil, errors.New("remote marker exceeded its size limit")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.done:
			select {
			case chunk := <-r.chunks:
				r.pending = append(r.pending, chunk...)
			default:
				return nil, errors.New("remote session ended before the next bootstrap step")
			}
		case chunk := <-r.chunks:
			r.pending = append(r.pending, chunk...)
		}
	}
}

func (r *sshMarkerReader) wait(ctx context.Context, markers ...string) (string, error) {
	for {
		for _, marker := range markers {
			needle := []byte(marker)
			if index := bytes.Index(r.pending, needle); index >= 0 {
				r.pending = append(r.pending[:index], r.pending[index+len(needle):]...)
				return marker, nil
			}
		}
		if len(r.pending) > 16<<10 {
			r.pending = append([]byte(nil), r.pending[len(r.pending)-(4<<10):]...)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-r.done:
			select {
			case chunk := <-r.chunks:
				r.pending = append(r.pending, chunk...)
			default:
				return "", errors.New("remote session ended before the next bootstrap step")
			}
		case chunk := <-r.chunks:
			r.pending = append(r.pending, chunk...)
		}
	}
}

func parseAtlasSSHAllowedCIDRs(raw string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("invalid ATLAS_SSH_ALLOWED_CIDRS entry %q", item)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	if len(prefixes) == 0 {
		return nil, errors.New("ATLAS_SSH_ALLOWED_CIDRS must contain at least one private network")
	}
	return prefixes, nil
}

func (a *App) atlasSSHAddress(rawIP string) (string, error) {
	address, err := netip.ParseAddr(strings.TrimSpace(rawIP))
	if err != nil {
		return "", errors.New("invalid_device_ip")
	}
	address = address.Unmap()
	if !address.IsPrivate() || address.IsLoopback() || address.IsUnspecified() || address.IsMulticast() || address.IsLinkLocalUnicast() {
		return "", errors.New("ssh_target_not_allowed")
	}
	allowed := false
	for _, prefix := range a.config.AtlasSSHAllowedCIDRs {
		if prefix.Contains(address) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", errors.New("ssh_target_not_allowed")
	}
	return net.JoinHostPort(address.String(), sshPort), nil
}

func (a *App) getProvisioningSSHHostKey(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	device, ok := a.findOwnedActivatableDevice(c, c.Param("device_id"))
	if !ok {
		return
	}
	if _, err := a.atlasSSHAddress(device.IP); err != nil {
		writeError(c, http.StatusUnprocessableEntity, err.Error(), "SSH 只允许连接已登记且位于允许园区网段内的设备地址")
		return
	}
	if !a.activationLimiter.allow("ssh-probe:"+accountID(c).String()+":"+device.ID.String(), time.Now().UTC()) {
		writeError(c, http.StatusTooManyRequests, "rate_limited", "连接检查过于频繁，请稍后重试")
		return
	}
	fingerprint, err := probeSSHHostKey(device)
	if err != nil {
		writeError(c, http.StatusBadGateway, "ssh_host_key_unavailable", "无法读取设备 SSH 指纹，请确认设备在线且 SSH 服务可达")
		return
	}
	if device.SSHHostKeyFingerprint != "" && !constantStringEqual(device.SSHHostKeyFingerprint, fingerprint) {
		writeError(c, http.StatusConflict, "ssh_host_key_changed", "设备 SSH 指纹与已登记值不同，请联系管理员核对设备或执行受控的密钥更换流程")
		return
	}
	c.JSON(http.StatusOK, gin.H{"fingerprint": fingerprint, "pinned": device.SSHHostKeyFingerprint != ""})
}

func probeSSHHostKey(device Device) (fingerprint string, err error) {
	address, err := netip.ParseAddr(strings.TrimSpace(device.IP))
	if err != nil {
		return "", err
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(address.String(), sshPort), sshProbeTimeout)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(sshProbeTimeout))
	config := &ssh.ClientConfig{
		User:    "yunshao-host-key-probe",
		Timeout: sshProbeTimeout,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			fingerprint = ssh.FingerprintSHA256(key)
			if device.SSHHostKeyFingerprint != "" && !constantStringEqual(device.SSHHostKeyFingerprint, fingerprint) {
				return errors.New("ssh host key changed")
			}
			return nil
		},
	}
	clientConn, channels, requests, _ := ssh.NewClientConn(conn, net.JoinHostPort(address.String(), sshPort), config)
	if clientConn != nil {
		client := ssh.NewClient(clientConn, channels, requests)
		_ = client.Close()
	}
	if fingerprint == "" {
		return "", errors.New("SSH host key was not presented")
	}
	return fingerprint, nil
}

func (a *App) startProvisioning(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	device, ok := a.findOwnedActivatableDevice(c, c.Param("device_id"))
	if !ok {
		return
	}
	if !device.Active && device.ActivationState != "expired" {
		writeError(c, http.StatusConflict, "device_disabled", "已停用设备不能通过首次接入恢复")
		return
	}
	if a.broker == nil || a.config.AtlasActivationOrigin == "" || len(a.config.AtlasSSHAllowedCIDRs) == 0 {
		writeError(c, http.StatusServiceUnavailable, "provisioning_unavailable", "设备接入服务尚未完成部署配置")
		return
	}
	if _, err := a.atlasSSHAddress(device.IP); err != nil {
		writeError(c, http.StatusUnprocessableEntity, err.Error(), "SSH 只允许连接已登记且位于允许园区网段内的设备地址")
		return
	}
	var existing BrokerCredential
	if err := a.db.WithContext(c.Request.Context()).Where("device_id = ?", device.ID).Take(&existing).Error; err == nil {
		writeError(c, http.StatusConflict, "device_already_provisioned", "设备已完成可信接入，请直接使用网页激活或续期")
		return
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "设备接入状态暂时无法读取")
		return
	}
	var input sshProvisionInput
	if decodeJSON(c, &input) != nil || !linuxUsernamePattern.MatchString(input.Username) || len(input.Password) == 0 || len(input.Password) > maxSSHPasswordSize ||
		strings.ContainsAny(input.Password, "\r\n\x00") || !sshFingerprintPattern.MatchString(input.HostKeyFingerprint) || input.DurationSeconds != nil && (*input.DurationSeconds < 1 || *input.DurationSeconds > maxActivationDurationSeconds) {
		writeError(c, http.StatusUnprocessableEntity, "invalid_request", "请填写有效的 SSH 用户名、密码、设备指纹和使用期限")
		return
	}
	if device.SSHHostKeyFingerprint != "" && !constantStringEqual(device.SSHHostKeyFingerprint, input.HostKeyFingerprint) {
		input.Password = ""
		writeError(c, http.StatusConflict, "ssh_host_key_changed", "设备 SSH 指纹与已登记值不同，请联系管理员核对设备或执行受控的密钥更换流程")
		return
	}
	apiCA, err := readProvisioningCA(a.config.AtlasActivationCAFile)
	if err != nil {
		input.Password = ""
		writeError(c, http.StatusServiceUnavailable, "provisioning_unavailable", "设备 HTTPS CA 未配置，请联系平台管理员检查 Web 证书")
		return
	}
	mqttCA, err := readProvisioningCA(a.config.BrokerCAFile)
	if err != nil {
		input.Password = ""
		writeError(c, http.StatusServiceUnavailable, "provisioning_unavailable", "设备 MQTT CA 未配置，请联系平台管理员检查 broker 证书")
		return
	}
	bundle, err := os.ReadFile(a.config.AtlasBootstrapBundleFile)
	if err != nil || len(bundle) == 0 || len(bundle) > maxBootstrapBundleSize {
		input.Password = ""
		writeError(c, http.StatusServiceUnavailable, "provisioning_unavailable", "Atlas agent 部署包不可用，请联系平台管理员重新部署 device-service")
		return
	}
	if !a.activationLimiter.allow("ssh-bootstrap:"+accountID(c).String()+":"+device.ID.String(), time.Now().UTC()) {
		input.Password = ""
		writeError(c, http.StatusTooManyRequests, "rate_limited", "设备接入操作过于频繁，请稍后重试")
		return
	}
	a.provisionMu.Lock()
	if a.activeProvisioning[device.ID] {
		a.provisionMu.Unlock()
		input.Password = ""
		writeError(c, http.StatusConflict, "provisioning_in_progress", "设备正在初始化，请等待当前任务结束")
		return
	}
	now := time.Now().UTC()
	workspaceID := accountID(c)
	job := ProvisioningJob{ID: uuid.New(), AccountID: workspaceID, WorkspaceID: &workspaceID, DeviceID: device.ID, Status: "queued", Stage: "queued", Message: "等待连接设备", CreatedAt: now, UpdatedAt: now}
	err = a.db.WithContext(c.Request.Context()).Transaction(func(tx *gorm.DB) error {
		var current Device
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND account_id = ? AND (active = ? OR activation_state = ?)", device.ID, accountID(c), true, "expired").Take(&current).Error; err != nil {
			return err
		}
		if err := tx.Model(&ProvisioningJob{}).Where("device_id = ? AND status IN ? AND updated_at <= ?", device.ID, []string{"queued", "running"}, now.Add(-provisioningJobTimeout)).Updates(map[string]any{
			"status": "failed", "stage": "failed", "error_code": "worker_interrupted", "message": "接入任务中断，请重新开始", "updated_at": now, "finished_at": now,
		}).Error; err != nil {
			return err
		}
		var inFlight ProvisioningJob
		if err := tx.Where("device_id = ? AND status IN ?", device.ID, []string{"queued", "running"}).Take(&inFlight).Error; err == nil {
			return errProvisioningInProgress
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(&job).Error
	})
	if err != nil {
		a.provisionMu.Unlock()
		input.Password = ""
		if errors.Is(err, errProvisioningInProgress) || errors.Is(err, gorm.ErrDuplicatedKey) {
			writeError(c, http.StatusConflict, "provisioning_in_progress", "设备正在初始化，请等待当前任务结束")
			return
		}
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "设备接入任务暂时无法创建")
		return
	}
	a.activeProvisioning[device.ID] = true
	a.provisionMu.Unlock()
	credentials := provisioningCredentials{username: input.Username, password: []byte(input.Password), fingerprint: input.HostKeyFingerprint}
	input.Password = ""
	input.Username = ""
	go a.runProvisioning(job, device, credentials, input.DurationSeconds, bundle, apiCA, mqttCA)
	c.JSON(http.StatusAccepted, job)
}

func readProvisioningCA(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 || len(data) > 1<<20 {
		return nil, errors.New("CA file unavailable")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("CA file is not a PEM certificate bundle")
	}
	return data, nil
}

func (a *App) getProvisioningJob(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	device, ok := a.findOwnedActivatableDevice(c, c.Param("device_id"))
	if !ok {
		return
	}
	jobID, err := uuid.Parse(c.Param("job_id"))
	if err != nil || jobID == uuid.Nil {
		writeError(c, http.StatusNotFound, "not_found", "设备接入任务不存在或不可访问")
		return
	}
	var job ProvisioningJob
	if err := a.db.WithContext(c.Request.Context()).Where("id = ? AND account_id = ? AND device_id = ?", jobID, accountID(c), device.ID).Take(&job).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			writeError(c, http.StatusNotFound, "not_found", "设备接入任务不存在或不可访问")
		} else {
			writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "设备接入任务暂时无法读取")
		}
		return
	}
	c.JSON(http.StatusOK, job)
}

func (a *App) getLatestProvisioningJob(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	device, ok := a.findOwnedActivatableDevice(c, c.Param("device_id"))
	if !ok {
		return
	}
	var job ProvisioningJob
	err := a.db.WithContext(c.Request.Context()).Where("account_id = ? AND device_id = ?", accountID(c), device.ID).
		Order("created_at DESC").Take(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusOK, gin.H{"job": nil})
		return
	}
	if err != nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "设备接入任务暂时无法读取")
		return
	}
	if (job.Status == "queued" || job.Status == "running") && job.UpdatedAt.Before(time.Now().UTC().Add(-provisioningJobTimeout)) {
		a.finishProvisioningJob(job.ID, false, "worker_interrupted", "接入任务中断，请重新开始")
		job.Status = "failed"
		job.Stage = "failed"
		job.ErrorCode = "worker_interrupted"
		job.Message = "接入任务中断，请重新开始"
	}
	c.JSON(http.StatusOK, gin.H{"job": job})
}

func (a *App) runProvisioning(job ProvisioningJob, device Device, credentials provisioningCredentials, duration *int64, bundle, apiCA, mqttCA []byte) {
	defer func() {
		for i := range credentials.password {
			credentials.password[i] = 0
		}
		a.provisionMu.Lock()
		delete(a.activeProvisioning, device.ID)
		a.provisionMu.Unlock()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), provisioningJobTimeout)
	defer cancel()
	if err := a.executeProvisioning(ctx, job, device, credentials, duration, bundle, apiCA, mqttCA); err != nil {
		code, message := provisioningSafeError(err)
		a.finishProvisioningJob(job.ID, false, code, message)
		return
	}
	a.finishProvisioningJob(job.ID, true, "", "设备接入完成，等待设备上线")
}

func (a *App) executeProvisioning(ctx context.Context, job ProvisioningJob, device Device, credentials provisioningCredentials, duration *int64, bundle, apiCA, mqttCA []byte) error {
	address, err := a.atlasSSHAddress(device.IP)
	if err != nil {
		return err
	}
	if err := a.setProvisioningStage(job.ID, "connecting", "正在安全连接设备"); err != nil {
		return err
	}
	conn, err := net.DialTimeout("tcp", address, sshConnectTimeout)
	if err != nil {
		return errors.New("ssh_unavailable")
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(provisioningJobTimeout))
	clientConfig := &ssh.ClientConfig{
		User:    credentials.username,
		Auth:    []ssh.AuthMethod{ssh.Password(string(credentials.password))},
		Timeout: sshConnectTimeout,
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			observed := ssh.FingerprintSHA256(key)
			if subtle.ConstantTimeCompare([]byte(observed), []byte(credentials.fingerprint)) != 1 {
				return errors.New("ssh_host_key_mismatch")
			}
			if device.SSHHostKeyFingerprint != "" && subtle.ConstantTimeCompare([]byte(observed), []byte(device.SSHHostKeyFingerprint)) != 1 {
				return errors.New("ssh_host_key_changed")
			}
			return nil
		},
	}
	sshConn, channels, requests, err := ssh.NewClientConn(conn, address, clientConfig)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unable to authenticate") || strings.Contains(strings.ToLower(err.Error()), "permission denied") {
			return errors.New("ssh_auth_failed")
		}
		if strings.Contains(err.Error(), "ssh_host_key_") {
			return errors.New("ssh_host_key_mismatch")
		}
		return errors.New("ssh_unavailable")
	}
	client := ssh.NewClient(sshConn, channels, requests)
	defer client.Close()
	if err := a.pinSSHHostKey(ctx, job.AccountID, device.ID, credentials.fingerprint); err != nil {
		return errors.New("ssh_host_key_changed")
	}
	clientConfig.Auth = nil

	scriptBytes, err := atlasBootstrapScript.ReadFile("atlas_bootstrap.sh")
	if err != nil {
		return errors.New("bootstrap_script_unavailable")
	}
	workDir := "/tmp/yunshao-bootstrap-" + uuid.NewString()
	command := buildAtlasBootstrapCommand(string(scriptBytes), device.ID.String(), a.config.AtlasActivationOrigin, workDir, len(bundle))
	session, err := client.NewSession()
	if err != nil {
		return errors.New("ssh_unavailable")
	}
	defer session.Close()
	stdin, err := session.StdinPipe()
	if err != nil {
		return errors.New("ssh_unavailable")
	}
	output := &sshOutputWriter{chunks: make(chan []byte, 128)}
	session.Stdout = output
	session.Stderr = output
	if err := session.Start(command); err != nil {
		return errors.New("remote_execution_failed")
	}
	done := make(chan struct{})
	waitResult := make(chan error, 1)
	go func() {
		waitResult <- session.Wait()
		output.close()
		close(done)
	}()
	markers := &sshMarkerReader{chunks: output.chunks, done: done}
	marker, err := markers.wait(ctx, sshSudoPrompt, sshArchiveInputMarker)
	if err != nil {
		return provisioningWaitError(err, "remote_execution_failed")
	}
	if marker == sshSudoPrompt {
		if _, err := stdin.Write(append(credentials.password, '\n')); err != nil {
			return errors.New("ssh_auth_failed")
		}
		for i := range credentials.password {
			credentials.password[i] = 0
		}
		marker, err = markers.wait(ctx, sshArchiveInputMarker)
		if err != nil {
			return provisioningWaitError(err, "ssh_privilege_failed")
		}
	} else {
		for i := range credentials.password {
			credentials.password[i] = 0
		}
	}
	if _, err := stdin.Write(bundle); err != nil {
		return errors.New("remote_execution_failed")
	}
	if err := a.setProvisioningStage(job.ID, "installing", "正在检查并部署设备 agent"); err != nil {
		return err
	}
	for {
		marker, err = markers.wait(ctx,
			"__YUNSHAO_STAGE:checking_board__",
			"__YUNSHAO_STAGE:installing_agent__",
			"__YUNSHAO_STAGE:configuring_tls__",
			"__YUNSHAO_STAGE:activating__",
			sshCAInputMarker,
			sshTicketInputMarker,
			sshBootstrapDoneMarker,
			"__YUNSHAO_ERROR:")
		if err != nil {
			return provisioningWaitError(err, "remote_execution_failed")
		}
		switch {
		case marker == "__YUNSHAO_STAGE:checking_board__":
			_ = a.setProvisioningStage(job.ID, "checking", "正在检查 Atlas 与 CANN 环境")
		case marker == "__YUNSHAO_STAGE:installing_agent__":
			_ = a.setProvisioningStage(job.ID, "installing", "正在构建并安装 Yunshao agent")
		case marker == "__YUNSHAO_STAGE:configuring_tls__":
			_ = a.setProvisioningStage(job.ID, "configuring", "正在配置设备安全证书")
		case marker == sshCAInputMarker:
			payload := base64.StdEncoding.EncodeToString(apiCA) + "\n" + base64.StdEncoding.EncodeToString(mqttCA) + "\n"
			if _, err := io.WriteString(stdin, payload); err != nil {
				return errors.New("remote_execution_failed")
			}
		case marker == "__YUNSHAO_STAGE:activating__":
			_ = a.setProvisioningStage(job.ID, "activating", "正在签发设备身份并启动 agent")
		case marker == sshTicketInputMarker:
			if err := a.ensureDeviceStillProvisionable(ctx, job.AccountID, device.ID); err != nil {
				return errors.New("device_state_changed")
			}
			ticket, token, err := a.issueActivationTicket(ctx, job.AccountID, device.ID, duration)
			if err != nil {
				return err
			}
			ticketBytes := []byte(token)
			token = ""
			if _, err := stdin.Write(append(ticketBytes, '\n')); err != nil {
				for i := range ticketBytes {
					ticketBytes[i] = 0
				}
				a.revokeUnconsumedTicket(ticket.ID)
				return errors.New("remote_execution_failed")
			}
			for i := range ticketBytes {
				ticketBytes[i] = 0
			}
			for {
				marker, err = markers.wait(ctx, sshBootstrapDoneMarker, "__YUNSHAO_ERROR:")
				if err != nil {
					a.revokeUnconsumedTicket(ticket.ID)
					return provisioningWaitError(err, "activation_failed")
				}
				if marker == sshBootstrapDoneMarker {
					select {
					case waitErr := <-waitResult:
						if waitErr != nil {
							return errors.New("activation_failed")
						}
					case <-ctx.Done():
						return errors.New("activation_failed")
					}
					_ = a.setProvisioningStage(job.ID, "completed", "设备接入完成，等待设备上线")
					return nil
				}
				code, parseErr := readRemoteErrorMarker(ctx, markers)
				if parseErr != nil {
					a.revokeUnconsumedTicket(ticket.ID)
					return errors.New("activation_failed")
				}
				a.revokeUnconsumedTicket(ticket.ID)
				return errors.New(code)
			}
		case marker == sshBootstrapDoneMarker:
			if err := <-waitResult; err != nil {
				return errors.New("activation_failed")
			}
			_ = a.setProvisioningStage(job.ID, "completed", "设备接入完成，等待设备上线")
			return nil
		case marker == "__YUNSHAO_ERROR:":
			code, parseErr := readRemoteErrorMarker(ctx, markers)
			if parseErr != nil {
				return errors.New("remote_execution_failed")
			}
			return errors.New(code)
		}
	}
}

func provisioningWaitError(err error, fallback string) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return errors.New("provisioning_timeout")
	}
	if errors.Is(err, context.Canceled) {
		return errors.New("provisioning_cancelled")
	}
	return errors.New(fallback)
}

func readRemoteErrorMarker(ctx context.Context, markers *sshMarkerReader) (string, error) {
	value, err := markers.readUntil(ctx, "__")
	if err != nil || len(value) == 0 || len(value) > 64 {
		return "", errors.New("invalid remote error marker")
	}
	code := string(value)
	if !regexp.MustCompile(`^[a-z_]+$`).MatchString(code) {
		return "", errors.New("invalid remote error marker")
	}
	return code, nil
}

func buildAtlasBootstrapCommand(script, deviceID, origin, workDir string, archiveSize int) string {
	quotedScript := shellQuote(script)
	args := strings.Join([]string{shellQuote(deviceID), shellQuote(origin), shellQuote(workDir), fmt.Sprint(archiveSize)}, " ")
	rootCommand := "/bin/sh -c " + quotedScript + " sh " + args
	return "if [ \"$(id -u)\" -eq 0 ]; then exec " + rootCommand + "; else exec sudo -S -p '" + sshSudoPrompt + "' -- " + rootCommand + "; fi"
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (a *App) pinSSHHostKey(ctx context.Context, accountID, deviceID uuid.UUID, fingerprint string) error {
	result := a.db.WithContext(ctx).Model(&Device{}).
		Where("id = ? AND account_id = ? AND (COALESCE(ssh_host_key_fingerprint, '') = '' OR ssh_host_key_fingerprint = ?)", deviceID, accountID, fingerprint).
		Update("ssh_host_key_fingerprint", fingerprint)
	if result.Error != nil || result.RowsAffected != 1 {
		return errors.New("ssh host key changed")
	}
	return nil
}

func (a *App) ensureDeviceStillProvisionable(ctx context.Context, accountID, deviceID uuid.UUID) error {
	var device Device
	if err := a.db.WithContext(ctx).Where("id = ? AND account_id = ? AND (active = ? OR activation_state = ?)", deviceID, accountID, true, "expired").Take(&device).Error; err != nil {
		return err
	}
	var credential BrokerCredential
	if err := a.db.WithContext(ctx).Where("device_id = ?", deviceID).Take(&credential).Error; err == nil {
		return errors.New("device already provisioned")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

func (a *App) revokeUnconsumedTicket(ticketID uuid.UUID) {
	now := time.Now().UTC()
	_ = a.db.Model(&ActivationTicket{}).Where("id = ? AND consumed_at IS NULL AND revoked_at IS NULL", ticketID).Update("revoked_at", now).Error
}

func (a *App) setProvisioningStage(jobID uuid.UUID, stage, message string) error {
	return a.db.Model(&ProvisioningJob{}).Where("id = ? AND status IN ?", jobID, []string{"queued", "running"}).Updates(map[string]any{
		"status": "running", "stage": stage, "message": message, "updated_at": time.Now().UTC(),
	}).Error
}

func (a *App) finishProvisioningJob(jobID uuid.UUID, success bool, errorCode, message string) {
	now := time.Now().UTC()
	status, stage := "failed", "failed"
	if success {
		status, stage = "succeeded", "completed"
	}
	_ = a.db.Model(&ProvisioningJob{}).Where("id = ?", jobID).Updates(map[string]any{
		"status": status, "stage": stage, "error_code": errorCode, "message": message, "updated_at": now, "finished_at": now,
	}).Error
}

func provisioningSafeError(err error) (string, string) {
	code := err.Error()
	switch code {
	case "ssh_unavailable":
		return code, "无法连接设备 SSH，请确认板卡已开机并可从云端网络访问 TCP/22"
	case "ssh_auth_failed":
		return code, "SSH 登录失败，请核对管理员提供的用户名和密码"
	case "ssh_host_key_mismatch", "ssh_host_key_changed":
		return "ssh_host_key_changed", "设备 SSH 指纹发生变化，已停止连接；请让管理员核对设备身份"
	case "ssh_privilege_failed":
		return code, "账号已登录，但没有可用的 root 或 sudo 权限"
	case "cann_environment_missing":
		return code, "Atlas 尚未配置 CANN 运行环境，请联系管理员完成板端基础安装"
	case "unsupported_architecture":
		return code, "设备架构不受支持；此接入向导仅支持 Atlas 200I DK A2"
	case "build_dependencies_unavailable":
		return code, "设备无法安装构建依赖，请检查园区软件源或网络后重试"
	case "agent_build_failed":
		return code, "Yunshao agent 构建失败，请检查设备 CANN 开发环境和系统依赖"
	case "atlas_groups_missing":
		return code, "Atlas 缺少 CANN 运行用户组，请联系管理员修复 CANN 安装"
	case "agent_state_path_invalid":
		return code, "设备的 Yunshao 状态目录路径异常，请联系管理员检查 /var/lib/yunshao"
	case "activation_failed":
		return code, "设备身份签发或启动失败，请检查 Atlas 到云端 HTTPS 与 MQTT 网络后重试"
	case "device_state_changed":
		return code, "设备状态已变化，请刷新页面后重试"
	case "ssh_terminal_unavailable":
		return code, "SSH 服务不支持受控终端，请联系管理员检查板端 SSH 配置"
	case "activation rate limited":
		return "rate_limited", "设备接入操作过于频繁，请稍后重试"
	case "package_manager_unsupported":
		return code, "设备缺少可用的软件包管理器，无法自动安装 agent 构建依赖"
	case "systemd_unavailable":
		return code, "设备未运行 systemd，无法自动启用 Yunshao agent 服务"
	case "provisioning_timeout":
		return code, "设备接入超过 30 分钟仍未完成，请检查设备网络、软件源和 CANN 环境后重试"
	case "provisioning_cancelled":
		return code, "设备接入已中断，请刷新页面并重新检查设备状态"
	default:
		return "remote_execution_failed", "设备初始化未完成，请检查网络、root/sudo 权限和 Atlas 运行环境后重试"
	}
}

func constantStringEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
