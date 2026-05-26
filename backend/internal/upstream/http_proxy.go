package upstream

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"proxydeck/backend/internal/auth"
	"proxydeck/backend/internal/model"
)

func URLForNode(node model.ProxyNode, cipher *auth.Cipher) (*url.URL, error) {
	scheme, err := SchemeForNode(node)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(fmt.Sprintf("%s://%s:%d", scheme, node.Host, node.Port))
	if err != nil {
		return nil, err
	}
	user, err := cipher.Decrypt(node.UpstreamUsernameEnc)
	if err != nil {
		return nil, err
	}
	pass, err := cipher.Decrypt(node.UpstreamPasswordEnc)
	if err != nil {
		return nil, err
	}
	if user != "" || pass != "" {
		u.User = url.UserPassword(user, pass)
	}
	return u, nil
}

func SchemeForNode(node model.ProxyNode) (string, error) {
	switch normalizedProtocol(node.Protocol) {
	case "http":
		if node.TLSEnabled {
			return "https", nil
		}
		return "http", nil
	case "socks", "socks5":
		return "socks5", nil
	default:
		return "", fmt.Errorf("unsupported upstream protocol: %s", node.Protocol)
	}
}

func IsHTTPProxyProtocol(protocol string) bool {
	return normalizedProtocol(protocol) == "http"
}

func IsSOCKSProxyProtocol(protocol string) bool {
	switch normalizedProtocol(protocol) {
	case "socks", "socks5":
		return true
	default:
		return false
	}
}

func normalizedProtocol(protocol string) string {
	return strings.ToLower(strings.TrimSpace(protocol))
}

func ProxyAuthorization(node model.ProxyNode, cipher *auth.Cipher) (string, error) {
	user, err := cipher.Decrypt(node.UpstreamUsernameEnc)
	if err != nil {
		return "", err
	}
	pass, err := cipher.Decrypt(node.UpstreamPasswordEnc)
	if err != nil {
		return "", err
	}
	if user == "" && pass == "" {
		return "", nil
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+pass)), nil
}
