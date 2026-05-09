package alipay

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestCanonicalStringExcludesSignAndSignType(t *testing.T) {
	got := canonicalString(map[string]string{
		"method":    "alipay.trade.precreate",
		"app_id":    "app_123",
		"sign":      "ignored",
		"sign_type": "RSA2",
		"empty":     "",
	})
	want := "app_id=app_123&method=alipay.trade.precreate"
	if got != want {
		t.Fatalf("canonicalString() = %q, want %q", got, want)
	}
}

func TestPrecreateVerifiesResponseSignature(t *testing.T) {
	appKey := mustRSAKey(t)
	alipayKey := mustRSAKey(t)

	client := newTestClient(t, appKey, &alipayKey.PublicKey, func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		values, err := url.ParseQuery(string(body))
		if err != nil {
			return nil, err
		}
		params := map[string]string{}
		for k := range values {
			params[k] = values.Get(k)
		}
		if strings.Contains(canonicalString(params), "sign_type") {
			t.Errorf("request canonical string must not include sign_type")
		}
		if !verifyWithPublic(&appKey.PublicKey, canonicalString(params), params["sign"]) {
			return nil, fmt.Errorf("request signature failed")
		}

		response := `{"code":"10000","msg":"Success","out_trade_no":"R1","trade_no":"T1","qr_code":"https://qr.example"}`
		sign := signWithKey(t, alipayKey, response)
		return jsonResponse(fmt.Sprintf(`{"alipay_trade_precreate_response":%s,"sign":%q}`, response, sign)), nil
	})

	got, err := client.Precreate(context.Background(), PrecreateInput{
		OutTradeNo: "R1",
		Subject:    "测试充值",
		AmountFen:  1000,
	})
	if err != nil {
		t.Fatalf("Precreate() error = %v", err)
	}
	if got.QRCode != "https://qr.example" || got.TradeNo != "T1" {
		t.Fatalf("Precreate() = %+v", got)
	}
}

func TestPrecreateRejectsInvalidResponseSignature(t *testing.T) {
	appKey := mustRSAKey(t)
	alipayKey := mustRSAKey(t)

	client := newTestClient(t, appKey, &alipayKey.PublicKey, func(r *http.Request) (*http.Response, error) {
		_, _ = io.Copy(io.Discard, r.Body)
		response := `{"code":"10000","msg":"Success","out_trade_no":"R1","trade_no":"T1","qr_code":"https://qr.example"}`
		sign := signWithKey(t, alipayKey, `{"code":"10000","msg":"tampered"}`)
		return jsonResponse(fmt.Sprintf(`{"alipay_trade_precreate_response":%s,"sign":%q}`, response, sign)), nil
	})

	_, err := client.Precreate(context.Background(), PrecreateInput{
		OutTradeNo: "R1",
		Subject:    "测试充值",
		AmountFen:  1000,
	})
	if !errors.Is(err, ErrInvalidSign) {
		t.Fatalf("Precreate() error = %v, want ErrInvalidSign", err)
	}
}

func TestQueryVerifiesResponseSignatureAndParsesStatus(t *testing.T) {
	appKey := mustRSAKey(t)
	alipayKey := mustRSAKey(t)

	client := newTestClient(t, appKey, &alipayKey.PublicKey, func(r *http.Request) (*http.Response, error) {
		values, err := requestValues(r)
		if err != nil {
			return nil, err
		}
		if values.Get("method") != "alipay.trade.query" {
			return nil, fmt.Errorf("method = %q", values.Get("method"))
		}
		params := valuesToMap(values)
		if !verifyWithPublic(&appKey.PublicKey, canonicalString(params), params["sign"]) {
			return nil, fmt.Errorf("request signature failed")
		}
		response := `{"code":"10000","msg":"Success","out_trade_no":"R1","trade_no":"T1","trade_status":"TRADE_SUCCESS","total_amount":10.00,"buyer_user_id":"2088"}`
		sign := signWithKey(t, alipayKey, response)
		return jsonResponse(fmt.Sprintf(`{"alipay_trade_query_response":%s,"sign":%q}`, response, sign)), nil
	})

	got, err := client.Query(context.Background(), "R1")
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if got.TradeStatus != "TRADE_SUCCESS" || got.TotalAmount != "10.00" || got.BuyerID != "2088" {
		t.Fatalf("Query() = %+v", got)
	}
}

func TestCloseVerifiesResponseSignature(t *testing.T) {
	appKey := mustRSAKey(t)
	alipayKey := mustRSAKey(t)

	client := newTestClient(t, appKey, &alipayKey.PublicKey, func(r *http.Request) (*http.Response, error) {
		values, err := requestValues(r)
		if err != nil {
			return nil, err
		}
		if values.Get("method") != "alipay.trade.close" {
			return nil, fmt.Errorf("method = %q", values.Get("method"))
		}
		params := valuesToMap(values)
		if !verifyWithPublic(&appKey.PublicKey, canonicalString(params), params["sign"]) {
			return nil, fmt.Errorf("request signature failed")
		}
		response := `{"code":"10000","msg":"Success","out_trade_no":"R1","trade_no":"T1"}`
		sign := signWithKey(t, alipayKey, response)
		return jsonResponse(fmt.Sprintf(`{"alipay_trade_close_response":%s,"sign":%q}`, response, sign)), nil
	})

	got, err := client.Close(context.Background(), "R1")
	if err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if got.OutTradeNo != "R1" || got.TradeNo != "T1" {
		t.Fatalf("Close() = %+v", got)
	}
}

func TestParseNotifyIncludesSellerID(t *testing.T) {
	appKey := mustRSAKey(t)
	alipayKey := mustRSAKey(t)
	client := newTestClient(t, appKey, &alipayKey.PublicKey, func(r *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("unexpected request")
	})
	form := url.Values{
		"app_id":       {"app_123"},
		"seller_id":    {"seller_123"},
		"out_trade_no": {"R1"},
		"trade_no":     {"T1"},
		"trade_status": {"TRADE_SUCCESS"},
		"total_amount": {"10.00"},
		"sign_type":    {"RSA2"},
	}
	params := valuesToMap(form)
	form.Set("sign", signWithKey(t, alipayKey, canonicalString(params)))

	got, err := client.ParseNotify(form)
	if err != nil {
		t.Fatalf("ParseNotify() error = %v", err)
	}
	if got.SellerID != "seller_123" {
		t.Fatalf("SellerID = %q", got.SellerID)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func newTestClient(t *testing.T, appKey *rsa.PrivateKey, alipayPub *rsa.PublicKey, roundTrip roundTripFunc) *Client {
	t.Helper()
	client, err := NewClient(Config{
		AppID:        "app_123",
		PrivateKey:   pemPrivateKey(t, appKey),
		AlipayPubKey: pemPublicKey(t, alipayPub),
		GatewayURL:   "https://alipay.example/gateway.do",
		NotifyURL:    "https://www.example.com/notify",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.httpClient = &http.Client{Transport: roundTrip}
	return client
}

func requestValues(r *http.Request) (url.Values, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	return url.ParseQuery(string(body))
}

func valuesToMap(values url.Values) map[string]string {
	params := map[string]string{}
	for k := range values {
		params[k] = values.Get(k)
	}
	return params
}

func jsonResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func mustRSAKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	return key
}

func signWithKey(t *testing.T, key *rsa.PrivateKey, content string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatalf("rsa.SignPKCS1v15() error = %v", err)
	}
	return base64.StdEncoding.EncodeToString(sig)
}

func verifyWithPublic(pub *rsa.PublicKey, content, sign string) bool {
	sig, err := base64.StdEncoding.DecodeString(sign)
	if err != nil {
		return false
	}
	sum := sha256.Sum256([]byte(content))
	return rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig) == nil
}

func pemPrivateKey(t *testing.T, key *rsa.PrivateKey) string {
	t.Helper()
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	}))
}

func pemPublicKey(t *testing.T, pub *rsa.PublicKey) string {
	t.Helper()
	raw, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("x509.MarshalPKIXPublicKey() error = %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: raw}))
}
