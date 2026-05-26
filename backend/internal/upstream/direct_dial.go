package upstream

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	crand "crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"io"
	"net"
	"strings"
	"time"

	"proxydeck/backend/internal/auth"
	"proxydeck/backend/internal/model"

	"github.com/google/uuid"
	sscore "github.com/shadowsocks/go-shadowsocks2/core"
	sssocks "github.com/shadowsocks/go-shadowsocks2/socks"
	xproxy "golang.org/x/net/proxy"
)

func IsShadowsocksProtocol(protocol string) bool {
	switch normalizedProtocol(protocol) {
	case "ss", "shadowsocks":
		return true
	default:
		return false
	}
}

func IsTrojanProtocol(protocol string) bool {
	return normalizedProtocol(protocol) == "trojan"
}

func IsVLESSProtocol(protocol string) bool {
	return normalizedProtocol(protocol) == "vless"
}

func IsVMessProtocol(protocol string) bool {
	return normalizedProtocol(protocol) == "vmess"
}

func IsTargetDialProtocol(protocol string) bool {
	return IsSOCKSProxyProtocol(protocol) || IsShadowsocksProtocol(protocol) || IsTrojanProtocol(protocol) || IsVLESSProtocol(protocol) || IsVMessProtocol(protocol)
}

func DialTarget(ctx context.Context, node model.ProxyNode, cipher *auth.Cipher, target string, dialTO time.Duration) (net.Conn, error) {
	switch {
	case IsSOCKSProxyProtocol(node.Protocol):
		return dialSOCKSTarget(ctx, node, cipher, target, dialTO)
	case IsShadowsocksProtocol(node.Protocol):
		return dialShadowsocksTarget(ctx, node, cipher, target, dialTO)
	case IsTrojanProtocol(node.Protocol):
		return dialTrojanTarget(ctx, node, cipher, target, dialTO)
	case IsVLESSProtocol(node.Protocol):
		return dialVLESSTarget(ctx, node, cipher, target, dialTO)
	case IsVMessProtocol(node.Protocol):
		return dialVMessTarget(ctx, node, cipher, target, dialTO)
	default:
		return nil, fmt.Errorf("unsupported target dial protocol: %s", node.Protocol)
	}
}

func dialSOCKSTarget(ctx context.Context, node model.ProxyNode, cipher *auth.Cipher, target string, dialTO time.Duration) (net.Conn, error) {
	user, err := cipher.Decrypt(node.UpstreamUsernameEnc)
	if err != nil {
		return nil, err
	}
	pass, err := cipher.Decrypt(node.UpstreamPasswordEnc)
	if err != nil {
		return nil, err
	}
	var authConfig *xproxy.Auth
	if user != "" || pass != "" {
		authConfig = &xproxy.Auth{User: user, Password: pass}
	}
	baseDialer := &net.Dialer{Timeout: dialTO}
	dialer, err := xproxy.SOCKS5("tcp", fmt.Sprintf("%s:%d", node.Host, node.Port), authConfig, baseDialer)
	if err != nil {
		return nil, err
	}
	if contextDialer, ok := dialer.(xproxy.ContextDialer); ok {
		return contextDialer.DialContext(ctx, "tcp", target)
	}
	return dialer.Dial("tcp", target)
}

func dialShadowsocksTarget(ctx context.Context, node model.ProxyNode, cipher *auth.Cipher, target string, dialTO time.Duration) (net.Conn, error) {
	method, err := cipher.Decrypt(node.UpstreamUsernameEnc)
	if err != nil {
		return nil, err
	}
	secret, err := cipher.Decrypt(node.UpstreamPasswordEnc)
	if err != nil {
		return nil, err
	}
	method = strings.TrimSpace(method)
	secret = strings.TrimSpace(secret)
	if method == "" || secret == "" {
		return nil, fmt.Errorf("shadowsocks node missing method or password")
	}
	ciph, err := sscore.PickCipher(method, nil, secret)
	if err != nil {
		return nil, err
	}
	rawConn, err := (&net.Dialer{Timeout: dialTO}).DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", node.Host, node.Port))
	if err != nil {
		return nil, err
	}
	securedConn := ciph.StreamConn(rawConn)
	addr := sssocks.ParseAddr(target)
	if addr == nil {
		_ = securedConn.Close()
		return nil, fmt.Errorf("invalid target address: %s", target)
	}
	if _, err := securedConn.Write(addr); err != nil {
		_ = securedConn.Close()
		return nil, err
	}
	return securedConn, nil
}

func dialTrojanTarget(ctx context.Context, node model.ProxyNode, cipher *auth.Cipher, target string, dialTO time.Duration) (net.Conn, error) {
	secret, err := cipher.Decrypt(node.UpstreamPasswordEnc)
	if err != nil {
		return nil, err
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, fmt.Errorf("trojan node missing password")
	}
	serverName := strings.TrimSpace(node.ServerName)
	if serverName == "" {
		serverName = node.Host
	}
	rawConn, err := tls.DialWithDialer(&net.Dialer{Timeout: dialTO}, "tcp", fmt.Sprintf("%s:%d", node.Host, node.Port), &tls.Config{
		ServerName:         serverName,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: node.TLSSkipVerify,
	})
	if err != nil {
		return nil, err
	}
	addr := sssocks.ParseAddr(target)
	if addr == nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("invalid target address: %s", target)
	}
	frame := buildTrojanConnectFrame(secret, addr)
	if _, err := rawConn.Write(frame); err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	return rawConn, nil
}

func buildTrojanConnectFrame(secret string, addr sssocks.Addr) []byte {
	passwordHash := sha256.Sum224([]byte(secret))
	frame := make([]byte, 0, 56+2+1+len(addr)+2)
	frame = append(frame, []byte(hex.EncodeToString(passwordHash[:]))...)
	frame = append(frame, '\r', '\n', 0x01)
	frame = append(frame, addr...)
	frame = append(frame, '\r', '\n')
	return frame
}

func dialVLESSTarget(ctx context.Context, node model.ProxyNode, cipher *auth.Cipher, target string, dialTO time.Duration) (net.Conn, error) {
	uuidText, err := cipher.Decrypt(node.UpstreamUsernameEnc)
	if err != nil {
		return nil, err
	}
	userID, err := uuid.Parse(strings.TrimSpace(uuidText))
	if err != nil {
		return nil, err
	}
	addr := sssocks.ParseAddr(target)
	if addr == nil {
		return nil, fmt.Errorf("invalid target address: %s", target)
	}
	rawConn, err := dialMaybeTLS(ctx, node, dialTO)
	if err != nil {
		return nil, err
	}
	frame := buildVLESSConnectFrame(userID, addr)
	if _, err := rawConn.Write(frame); err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	if err := consumeVLESSResponse(rawConn); err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	return rawConn, nil
}

func dialMaybeTLS(ctx context.Context, node model.ProxyNode, dialTO time.Duration) (net.Conn, error) {
	addr := fmt.Sprintf("%s:%d", node.Host, node.Port)
	if node.TLSEnabled {
		serverName := strings.TrimSpace(node.ServerName)
		if serverName == "" {
			serverName = node.Host
		}
		return tls.DialWithDialer(&net.Dialer{Timeout: dialTO}, "tcp", addr, &tls.Config{
			ServerName:         serverName,
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: node.TLSSkipVerify,
		})
	}
	return (&net.Dialer{Timeout: dialTO}).DialContext(ctx, "tcp", addr)
}

func buildVLESSConnectFrame(userID uuid.UUID, addr sssocks.Addr) []byte {
	frame := make([]byte, 0, 1+16+1+1+len(addr))
	frame = append(frame, 0x01)
	frame = append(frame, userID[:]...)
	frame = append(frame, 0x00)
	frame = append(frame, 0x01)
	frame = append(frame, addr...)
	return frame
}

func consumeVLESSResponse(conn net.Conn) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return err
	}
	addonLen := int(header[1])
	if addonLen == 0 {
		return nil
	}
	addons := make([]byte, addonLen)
	_, err := io.ReadFull(conn, addons)
	return err
}

func dialVMessTarget(ctx context.Context, node model.ProxyNode, cipherSvc *auth.Cipher, target string, dialTO time.Duration) (net.Conn, error) {
	uuidText, err := cipherSvc.Decrypt(node.UpstreamUsernameEnc)
	if err != nil {
		return nil, err
	}
	userID, err := uuid.Parse(strings.TrimSpace(uuidText))
	if err != nil {
		return nil, err
	}
	addr := sssocks.ParseAddr(target)
	if addr == nil {
		return nil, fmt.Errorf("invalid target address: %s", target)
	}
	rawConn, err := dialMaybeTLS(ctx, node, dialTO)
	if err != nil {
		return nil, err
	}
	req, respKey, respIV, err := buildVMessConnectFrame(userID, addr)
	if err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	if _, err := rawConn.Write(req); err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	if err := consumeVMessResponse(rawConn, respKey, respIV); err != nil {
		_ = rawConn.Close()
		return nil, err
	}
	return rawConn, nil
}

func buildVMessConnectFrame(userID uuid.UUID, addr sssocks.Addr) ([]byte, []byte, []byte, error) {
	const authInfoSize = 16
	bodyKey := make([]byte, 16)
	bodyIV := make([]byte, 16)
	if _, err := io.ReadFull(crand.Reader, bodyKey); err != nil {
		return nil, nil, nil, err
	}
	if _, err := io.ReadFull(crand.Reader, bodyIV); err != nil {
		return nil, nil, nil, err
	}
	respVBuf := []byte{0}
	if _, err := io.ReadFull(crand.Reader, respVBuf); err != nil {
		return nil, nil, nil, err
	}
	respV := respVBuf[0]
	respHeaderKey := md5Sum(bodyKey)
	respHeaderIV := md5Sum(bodyIV)
	authInfo := buildVMessAuthInfo(userID)
	bodyPlain := make([]byte, 0, 64+len(addr))
	bodyPlain = append(bodyPlain, 0x01)
	bodyPlain = append(bodyPlain, bodyIV...)
	bodyPlain = append(bodyPlain, bodyKey...)
	bodyPlain = append(bodyPlain, respV)
	bodyPlain = append(bodyPlain, 0x00)
	bodyPlain = append(bodyPlain, 0x00, 0x00)
	bodyPlain = append(bodyPlain, 0x00)
	bodyPlain = append(bodyPlain, 0x06)
	bodyPlain = append(bodyPlain, 0x00)
	bodyPlain = append(bodyPlain, addr...)
	checksum := fnv1a32(bodyPlain)
	bodyPlain = append(bodyPlain, checksum...)
	bodyCipherKey := md5Sum(append(userID[:], []byte("c48619fe-8f02-49e0-b9e9-edf763e17e21")...))
	bodyCipherIV := md5Sum(append(append([]byte{}, authInfo[:]...), authInfo[:]...))
	encryptedBody, err := aesCFBEncrypt(bodyPlain, bodyCipherKey, bodyCipherIV)
	if err != nil {
		return nil, nil, nil, err
	}
	packet := make([]byte, 0, len(authInfo)+len(encryptedBody))
	packet = append(packet, authInfo[:]...)
	packet = append(packet, encryptedBody...)
	return packet, respHeaderKey, respHeaderIV, nil
}

func buildVMessAuthInfo(userID uuid.UUID) [16]byte {
	var authInfo [16]byte
	ts := time.Now().UTC().Unix()
	tsBytes := []byte{
		byte(ts >> 56), byte(ts >> 48), byte(ts >> 40), byte(ts >> 32),
		byte(ts >> 24), byte(ts >> 16), byte(ts >> 8), byte(ts),
	}
	hash := md5Sum(append(userID[:], tsBytes...))
	copy(authInfo[:], hash)
	return authInfo
}

func consumeVMessResponse(conn net.Conn, key, iv []byte) error {
	respHeader := make([]byte, 4)
	if _, err := io.ReadFull(conn, respHeader); err != nil {
		return err
	}
	plain, err := aesCFBDecrypt(respHeader, key, iv)
	if err != nil {
		return err
	}
	extraLen := int(plain[3])
	if extraLen == 0 {
		return nil
	}
	extra := make([]byte, extraLen)
	if _, err := io.ReadFull(conn, extra); err != nil {
		return err
	}
	_, err = aesCFBDecrypt(extra, key, iv)
	return err
}

func aesCFBEncrypt(plain, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(plain))
	cipher.NewCFBEncrypter(block, iv).XORKeyStream(out, plain)
	return out, nil
}

func aesCFBDecrypt(cipherText, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	out := make([]byte, len(cipherText))
	cipher.NewCFBDecrypter(block, iv).XORKeyStream(out, cipherText)
	return out, nil
}

func md5Sum(data []byte) []byte {
	sum := md5.Sum(data)
	return sum[:]
}

func fnv1a32(data []byte) []byte {
	h := fnv.New32a()
	_, _ = h.Write(data)
	sum := h.Sum32()
	return []byte{byte(sum >> 24), byte(sum >> 16), byte(sum >> 8), byte(sum)}
}
