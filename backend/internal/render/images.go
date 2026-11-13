package render

import (
	"bytes"
	"context"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

// FetchImage pins the connection to a validated public IP to prevent SSRF and DNS rebinding.
func FetchImage(ctx context.Context, source string) ([]byte, error) {
	transport := &http.Transport{TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("image host has no public address")
		}
		for _, ip := range ips {
			if !publicIP(ip.IP) {
				return nil, fmt.Errorf("image host is not public")
			}
		}
		return (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
	}
	defer transport.CloseIdleConnections()
	valid := func(raw string) bool {
		u, err := url.Parse(raw)
		return err == nil && u.Scheme == "https" && u.User == nil && u.Hostname() != "" && (u.Port() == "" || u.Port() == "443")
	}
	if !valid(source) {
		return nil, fmt.Errorf("invalid image URL")
	}
	client := &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !valid(r.URL.String()) {
			return fmt.Errorf("invalid image redirect")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(resp.Body, (8<<20)+1))
	closeErr := resp.Body.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if resp.StatusCode != http.StatusOK || len(data) > 8<<20 {
		return nil, fmt.Errorf("image download failed")
	}
	return data, nil
}
func publicIP(ip net.IP) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, cidr := range []string{"100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96"} {
		_, n, _ := net.ParseCIDR(cidr)
		if n.Contains(ip) {
			return false
		}
	}
	return true
}
func DecodeImage(data []byte) (Image, error) {
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 8_000_000 {
		return Image{}, fmt.Errorf("invalid image dimensions or format")
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Image{}, err
	}
	var normalized bytes.Buffer
	if err := png.Encode(&normalized, decoded); err != nil {
		return Image{}, err
	}
	return Image{normalized.Bytes(), config.Width, config.Height}, nil
}
