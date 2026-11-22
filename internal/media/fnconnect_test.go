package media

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

type fnTestTransport func(*http.Request) (*http.Response, error)

func (f fnTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFNConnectPortalSignatures(t *testing.T) {
	headers := make(http.Header)
	signFNConnectLookup(headers, "living-room7", []byte(`{"fnId":"living-room7"}`), time.Unix(1700000000, 123456789))
	if got := headers.Get("Fn-Sign"); got != "8db71fa0ac45b085f8d37da337853e736d6d7c9cd35bd7007446dbcf3e5b72b6" {
		t.Errorf("FN-Sign = %q", got)
	}
	if got := headers.Get("Authx"); got != "nonce=256789&timestamp=1700000000123&sign=0cddb9be33432996171b2d54d4dc1eef" {
		t.Errorf("authx = %q", got)
	}
}

func TestFNConnectSelectsVerifiedRoute(t *testing.T) {
	const routes = `{"code":0,"data":{"ipv4":["192.168.2.30","127.0.0.1"],"ddns":["direct.example.test"],
		"publicIpv4":["203.0.113.9"],"publicIpv6":["2001:db8::2"],"fn":["relay.example.test"],"port":{"httpPort":"8005","httpsPort":5667}}}`
	for _, test := range []struct {
		name, address string
	}{
		{"private direct", "http://192.168.2.30:8005/v"},
		{"secure DDNS", "https://direct.example.test:5667/v"},
		{"secure public IPv4", "https://203.0.113.9:5667/v"},
		{"secure public IPv6", "https://[2001:db8::2]:5667/v"},
		{"relay when direct paths fail", "https://relay.example.test/v"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := FNConnectClient{Transport: fnTestTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Error("the route probe sent NAS credentials")
				}
				if r.URL.Host == "fnos.net" {
					assertFNConnectLookup(t, r)
					return fnTestReply(r, 200, routes), nil
				}
				if r.URL.Hostname() == "127.0.0.1" || r.URL.Scheme == "http" && r.URL.Hostname() == "203.0.113.9" {
					t.Errorf("unexpected route: %s", r.URL)
				}
				if r.URL.String() == test.address+"/api/v1/sys/config" {
					return fnTestReply(r, 200, `{"code":0,"data":{"server_guid":"sample-nas","server_name":"Fixture NAS"}}`), nil
				}
				return fnTestReply(r, 404, ""), nil
			})}
			result, err := client.Resolve(t.Context(), " Living-Room7 ", "")
			if err != nil || result.Address != test.address || result.Name != "Fixture NAS" || result.Cookie != "" {
				t.Fatalf("route = %+v, %v", result, err)
			}
		})
	}
}

func TestFNConnectVerifiesRelayBackupIdentity(t *testing.T) {
	const response = `{"code":0,"data":{"ipv4":["192.168.2.30"],"fn":["relay.example.test"]}}`
	const relay = "https://relay.example.test/v"
	const direct = "http://192.168.2.30/v"
	for _, id := range []string{"same-nas", "another-nas", ""} {
		client := FNConnectClient{Transport: fnTestTransport(func(r *http.Request) (*http.Response, error) {
			switch r.URL.String() {
			case fnConnectPortal + fnConnectLookup:
				return fnTestReply(r, 200, response), nil
			case direct + "/api/v1/sys/config":
				return fnTestReply(r, 200, `{"code":0,"data":{"server_guid":"same-nas"}}`), nil
			case relay + "/api/v1/sys/config":
				return fnTestReply(r, 200, `{"code":0,"data":{"server_guid":"`+id+`","server_name":"Relay NAS"}}`), nil
			default:
				return fnTestReply(r, 404, ""), nil
			}
		})}
		result, err := client.Resolve(t.Context(), "living-room7", "")
		wantAddress, wantBackup := direct, ""
		switch id {
		case "same-nas":
			wantBackup = relay
		case "another-nas":
			wantAddress = relay
		}
		if err != nil || result.Address != wantAddress || result.BackupAddress != wantBackup {
			t.Fatalf("routes for %q = %+v, %v", id, result, err)
		}
	}
}

func TestFNConnectAccessCodeIsScopedToTheSelectedGateway(t *testing.T) {
	const routes = `{"code":0,"data":{"ipv4":["192.168.2.30"],"fn":["relay.example.test","other.example.test"]}}`
	tests := []struct {
		name string
		code string
		want error
	}{
		{"approved code and redirect", "right + code", nil},
		{"incorrect code", "incorrect", ErrFNConnectCode},
		{"redirect to other host", "foreign", ErrFNConnectCode},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var mu sync.Mutex
			var submitted []string
			client := FNConnectClient{Transport: fnTestTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("Authorization") != "" {
					t.Error("the discovery request carried a NAS token")
				}
				if r.URL.Host == "fnos.net" {
					assertFNConnectLookup(t, r)
					if r.Header.Get("Cookie") != "" {
						t.Error("the portal request carried an access grant")
					}
					return fnTestReply(r, 200, routes), nil
				}
				if r.URL.Scheme != "https" {
					t.Errorf("access-code flow used HTTP: %s", r.URL.Host)
				}
				if r.URL.Host != "relay.example.test" {
					if r.URL.Path != "/trimcon" || r.Header.Get("Cookie") != "" {
						t.Error("the access grant reached another origin")
					}
					return fnTestReply(r, 404, ""), nil
				}
				switch r.URL.Path {
				case "/trimcon":
					if r.Header.Get("Cookie") != "" {
						t.Error("gateway health probe carried an access grant")
					}
					return fnTestReply(r, 204, ""), nil
				case "/c/" + test.code:
					mu.Lock()
					submitted = append(submitted, r.URL.Host)
					mu.Unlock()
					if test.code == "incorrect" {
						return fnTestReply(r, 404, ""), nil
					}
					response := fnTestReply(r, 302, "")
					redirect := "/"
					if test.code == "foreign" {
						redirect = "https://other.example.test/gate"
					}
					response.Header.Set("Location", redirect)
					response.Header.Add("Set-Cookie", "access=fixture-pass; Path=/; Secure; HttpOnly")
					response.Header.Add("Set-Cookie", "admin=unrelated; Path=/admin; Secure; HttpOnly")
					return response, nil
				case "/":
					if r.Header.Get("Referer") != "" {
						t.Error("redirect via access code leaked the entry URL in Referer")
					}
					return fnTestReply(r, 200, ""), nil
				case "/v/api/v1/sys/config":
					if r.Header.Get("Cookie") != "access=fixture-pass" {
						t.Errorf("the media endpoint received the wrong gateway cookie")
						return fnTestReply(r, 403, ""), nil
					}
					return fnTestReply(r, 200, `{"code":0,"data":{"server_guid":"sample-nas","server_name":"Fixture NAS"}}`), nil
				default:
					return fnTestReply(r, 404, ""), nil
				}
			})}
			result, err := client.Resolve(t.Context(), "living-room7", test.code)
			if !errors.Is(err, test.want) {
				t.Fatalf("Resolve error = %v, want %v", err, test.want)
			}
			if test.want == nil && (result.Address != "https://relay.example.test/v" || result.Cookie != "access=fixture-pass") {
				t.Fatalf("unexpected grant: %+v", result)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(submitted) != 1 || submitted[0] != "relay.example.test" {
				t.Fatalf("access code submit scope = %v", submitted)
			}
		})
	}
}

func TestFNConnectAbsentAndLookupFailure(t *testing.T) {
	for _, test := range []struct {
		body string
		want error
	}{
		{`{"code":3000006}`, ErrFNConnectAbsent},
		{`{"code":3000037,"msg":"Not Found Error"}`, ErrFNConnectAbsent},
		{`{"code":3000009}`, ErrFNConnectLookup},
		{`{"unrelated":"json"}`, ErrFNConnectLookup},
		{`{"code":0,"data":{"fn":["http://untrusted.example.test"]}}`, ErrFNConnectRoute},
	} {
		client := FNConnectClient{Transport: fnTestTransport(func(r *http.Request) (*http.Response, error) {
			assertFNConnectLookup(t, r)
			return fnTestReply(r, 200, test.body), nil
		})}
		if _, err := client.Resolve(t.Context(), "living-room7", ""); !errors.Is(err, test.want) {
			t.Errorf("Resolve(%s) = %v, want %v", test.body, err, test.want)
		}
	}
}

func assertFNConnectLookup(t *testing.T, r *http.Request) {
	t.Helper()
	var request map[string]string
	if r.URL.String() != fnConnectPortal+fnConnectLookup || r.Method != http.MethodPost ||
		r.Header.Get("Authx") == "" || r.Header.Get("Fn-Sign") == "" ||
		json.NewDecoder(r.Body).Decode(&request) != nil || len(request) != 1 || request["fnId"] != "living-room7" {
		t.Errorf("invalid FN Connect lookup for %s %s", r.Method, r.URL)
	}
}

func fnTestReply(r *http.Request, status int, body string) *http.Response {
	response := httptest.NewRecorder()
	response.WriteHeader(status)
	_, _ = io.WriteString(response, body)
	result := response.Result()
	result.Request = r
	return result
}

func TestFNConnectCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	client := FNConnectClient{Transport: fnTestTransport(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	cancel()
	if _, err := client.Resolve(ctx, "living-room7", ""); !errors.Is(err, ErrFNConnectLookup) {
		t.Fatalf("canceled lookup = %v", err)
	}
}
