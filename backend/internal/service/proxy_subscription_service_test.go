package service

import "testing"

func TestParseProxySubscriptionClashVLESS(t *testing.T) {
	raw := []byte(`
proxies:
  - name: vless-node
    type: vless
    server: example.com
    port: 443
    uuid: 00000000-0000-0000-0000-000000000000
    tls: true
  - name: direct-http
    type: http
    server: proxy.example.com
    port: 8080
`)
	nodes, err := ParseProxySubscription(raw)
	if err != nil {
		t.Fatalf("ParseProxySubscription() error = %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes len = %d, want 2", len(nodes))
	}
	if nodes[0].Type != "vless" || nodes[0].Server != "example.com" || nodes[0].Port != 443 {
		t.Fatalf("unexpected first node: %+v", nodes[0])
	}
	tunnel, direct := splitProxyNodes(nodes)
	if len(tunnel) != 1 || len(direct) != 1 {
		t.Fatalf("split tunnel/direct = %d/%d, want 1/1", len(tunnel), len(direct))
	}
}

func TestParseProxySubscriptionURIList(t *testing.T) {
	raw := []byte("vless://00000000-0000-0000-0000-000000000000@example.com:443?security=tls&type=ws&host=cdn.example.com&path=%2Fws#US%20VLESS\n")
	nodes, err := ParseProxySubscription(raw)
	if err != nil {
		t.Fatalf("ParseProxySubscription() error = %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes len = %d, want 1", len(nodes))
	}
	node := nodes[0]
	if node.Name != "US VLESS" || node.Type != "vless" || node.Server != "example.com" || node.Port != 443 {
		t.Fatalf("unexpected node: %+v", node)
	}
	if node.Raw["network"] != "ws" {
		t.Fatalf("network = %v, want ws", node.Raw["network"])
	}
}
