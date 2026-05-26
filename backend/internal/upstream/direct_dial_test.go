package upstream

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"testing"
	"time"

	"proxydeck/backend/internal/auth"
	"proxydeck/backend/internal/model"

	"github.com/google/uuid"
	sscore "github.com/shadowsocks/go-shadowsocks2/core"
	sssocks "github.com/shadowsocks/go-shadowsocks2/socks"
)

func TestDialTargetShadowsocks(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("tcp listen unavailable: %v", err)
	}
	defer listener.Close()

	cipherSvc, err := auth.NewCipher("test-secret")
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	methodEnc, _ := cipherSvc.Encrypt("DUMMY")
	passEnc, _ := cipherSvc.Encrypt("shadow-secret")
	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	port, err := net.LookupPort("tcp", portText)
	if err != nil {
		t.Fatalf("lookup port: %v", err)
	}
	node := model.ProxyNode{
		Protocol:            "ss",
		Host:                host,
		Port:                port,
		UpstreamUsernameEnc: methodEnc,
		UpstreamPasswordEnc: passEnc,
	}

	gotTarget := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		serverCipher, err := sscore.PickCipher("DUMMY", nil, "shadow-secret")
		if err != nil {
			t.Errorf("pick cipher: %v", err)
			return
		}
		securedConn := serverCipher.StreamConn(conn)
		addr, err := sssocks.ReadAddr(securedConn)
		if err != nil {
			t.Errorf("read addr: %v", err)
			return
		}
		gotTarget <- addr.String()
	}()

	clientConn, err := DialTarget(context.Background(), node, cipherSvc, "example.com:443", time.Second)
	if err != nil {
		t.Fatalf("dial target: %v", err)
	}
	_ = clientConn.Close()

	select {
	case got := <-gotTarget:
		if got != "example.com:443" {
			t.Fatalf("target = %q, want example.com:443", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for shadowsocks target")
	}
}

func TestBuildTrojanConnectFrame(t *testing.T) {
	addr := sssocks.ParseAddr("example.com:443")
	if addr == nil {
		t.Fatal("parse addr returned nil")
	}
	frame := buildTrojanConnectFrame("secret-pass", addr)
	if len(frame) != 56+2+1+len(addr)+2 {
		t.Fatalf("frame len = %d, want %d", len(frame), 56+2+1+len(addr)+2)
	}
	hash := sha256.Sum224([]byte("secret-pass"))
	wantPrefix := hex.EncodeToString(hash[:]) + "\r\n"
	if string(frame[:58]) != wantPrefix {
		t.Fatalf("frame prefix = %q, want %q", string(frame[:58]), wantPrefix)
	}
	if frame[58] != 0x01 {
		t.Fatalf("cmd = %x, want 0x01", frame[58])
	}
	if string(frame[len(frame)-2:]) != "\r\n" {
		t.Fatalf("frame suffix = %q, want CRLF", string(frame[len(frame)-2:]))
	}
}

func TestBuildVLESSConnectFrame(t *testing.T) {
	userID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	addr := sssocks.ParseAddr("example.com:443")
	if addr == nil {
		t.Fatal("parse addr returned nil")
	}
	frame := buildVLESSConnectFrame(userID, addr)
	if len(frame) != 1+16+1+1+len(addr) {
		t.Fatalf("frame len = %d", len(frame))
	}
	if frame[0] != 0x01 {
		t.Fatalf("version = %x, want 0x01", frame[0])
	}
	if !bytes.Equal(frame[1:17], userID[:]) {
		t.Fatal("uuid bytes mismatch")
	}
	if frame[17] != 0x00 || frame[18] != 0x01 {
		t.Fatalf("addon/cmd bytes = %x %x, want 00 01", frame[17], frame[18])
	}
	if !bytes.Equal(frame[19:], addr) {
		t.Fatal("address bytes mismatch")
	}
}

func TestConsumeVLESSResponse(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	done := make(chan error, 1)
	go func() {
		done <- consumeVLESSResponse(client)
	}()

	_, _ = server.Write([]byte{0x01, 0x03, 'a', 'b', 'c'})
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("consume response: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for response consumer")
	}

	buf := make([]byte, 1)
	server.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, err := server.Read(buf); err == nil || err == io.EOF {
		t.Fatal("expected no extra bytes to remain readable from consumed response")
	}
}

func TestBuildVMessConnectFrame(t *testing.T) {
	userID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	addr := sssocks.ParseAddr("example.com:443")
	if addr == nil {
		t.Fatal("parse addr returned nil")
	}
	packet, respKey, respIV, err := buildVMessConnectFrame(userID, addr)
	if err != nil {
		t.Fatalf("build vmess frame: %v", err)
	}
	if len(packet) <= 16 {
		t.Fatalf("packet too short: %d", len(packet))
	}
	if len(respKey) != 16 || len(respIV) != 16 {
		t.Fatalf("unexpected response key/iv lens: %d %d", len(respKey), len(respIV))
	}
}

func TestConsumeVMessResponse(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	key := bytes.Repeat([]byte{1}, 16)
	iv := bytes.Repeat([]byte{2}, 16)
	plain := []byte{0x01, 0x00, 0x00, 0x03, 'a', 'b', 'c'}
	encrypted, err := aesCFBEncrypt(plain, key, iv)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		done <- consumeVMessResponse(client, key, iv)
	}()
	_, _ = server.Write(encrypted)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("consume vmess response: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for vmess response consumer")
	}
}
