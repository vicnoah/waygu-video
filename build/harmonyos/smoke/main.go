//go:build js && wasm && nsgoharmony

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"

	"waygu-video/internal/media"
	"waygu-video/internal/mediatls"
	"waygu-video/internal/platformnet"
	"waygu-video/internal/playback"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if err := verifyDeadline(ctx); err != nil {
		return fmt.Errorf("socket deadline fixture: %w", err)
	}
	if err := verifyTLS(ctx); err != nil {
		return fmt.Errorf("TLS fixture: %w", err)
	}
	fmt.Println("TLS rejected an untrusted leaf and accepted the approved leaf")
	password := os.Getenv("WAYGU_TEST_PASSWORD")
	if password == "" {
		password = "waygu-fixture-local"
	}
	for _, fixture := range []struct {
		kind    media.Provider
		address string
	}{
		{media.Emby, "http://127.0.0.1:28096"},
		{media.Jellyfin, "http://127.0.0.1:18096"},
		{media.Plex, "http://127.0.0.1:32400"},
	} {
		if err := verifyServer(ctx, fixture.kind, fixture.address, password); err != nil {
			return fmt.Errorf("%s: %w", fixture.kind, err)
		}
		fmt.Printf("%s approved a session, listed the library and served an MP4 range and HLS segment through the playback proxy\n", fixture.kind)
	}
	return nil
}

func verifyDeadline(ctx context.Context) error {
	listener, err := platformnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer listener.Close()
	conn, err := platformnet.DialContext(ctx, "tcp", listener.Addr().String())
	if err != nil {
		return err
	}
	defer conn.Close()
	peer, err := listener.Accept()
	if err != nil {
		return err
	}
	defer peer.Close()
	var data [3]byte
	if err := conn.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		return err
	}
	if _, err := conn.Read(data[:]); !errors.Is(err, os.ErrDeadlineExceeded) {
		return fmt.Errorf("read did not report its deadline: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return err
	}
	if _, err := peer.Write([]byte("ack")); err != nil {
		return err
	}
	if _, err := io.ReadFull(conn, data[:]); err != nil {
		return err
	}
	if string(data[:]) != "ack" {
		return errors.New("timed-out read did not preserve the next bytes")
	}
	return nil
}

func verifyTLS(ctx context.Context) error {
	listener, err := platformnet.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	fixture := &httptest.Server{
		Listener: listener,
		Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})},
	}
	fixture.StartTLS()
	defer fixture.Close()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, fixture.URL, nil)
	if err != nil {
		return err
	}
	response, rejected := http.DefaultClient.Do(request)
	if rejected == nil {
		response.Body.Close()
		return errors.New("accepted an untrusted fixture certificate")
	}
	pin, ok := mediatls.Candidate(fixture.URL, rejected)
	if !ok {
		return errors.New("the authority failure did not expose the correct approval candidate")
	}
	transport, err := mediatls.Transport(fixture.URL, pin)
	if err != nil {
		return err
	}
	defer transport.CloseIdleConnections()
	response, err = (&http.Client{Transport: transport}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return fmt.Errorf("approved request returned HTTP %d", response.StatusCode)
	}
	return nil
}

func verifyServer(ctx context.Context, kind media.Provider, address, password string) error {
	device := media.NewDevice("Waygu HarmonyOS smoke")
	client, err := media.NewClient(kind, address, device, media.Session{})
	if err != nil {
		return err
	}
	session, err := client.SignIn(ctx, media.Credentials{Username: "waygu-test", Password: password})
	if err != nil {
		return fmt.Errorf("sign-in: %w", err)
	}
	client, err = media.NewClient(kind, address, device, session)
	if err != nil {
		return err
	}
	libraries, err := client.Libraries(ctx)
	if err != nil {
		return fmt.Errorf("libraries: %w", err)
	}
	for _, library := range libraries {
		if library.Name != "Movies" {
			continue
		}
		page, err := client.Catalog(ctx, media.Query{LibraryID: library.ID, Search: "Waygu Test Movie"})
		if err != nil {
			return fmt.Errorf("catalog: %w", err)
		}
		for _, item := range page.Items {
			if item.Title == "Waygu Test Movie" {
				stream, err := client.Playback(ctx, item, media.PlaybackOptions{})
				if err != nil {
					return fmt.Errorf("playback: %w", err)
				}
				if err := verifyRange(ctx, stream); err != nil {
					return err
				}
				transcoded, err := client.Playback(ctx, item, media.PlaybackOptions{MaxBitrate: 300_000})
				if err != nil {
					return fmt.Errorf("start HLS: %w", err)
				}
				return verifyHLS(ctx, transcoded)
			}
		}
	}
	return errors.New("movie fixture is missing")
}

func verifyRange(ctx context.Context, stream media.Stream) error {
	url, close, err := playback.Open(stream.Resource())
	if err != nil {
		return err
	}
	defer close()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Range", "bytes=0-1023")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return errors.New("playback proxy read failed")
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1025))
	if err != nil || response.StatusCode != http.StatusPartialContent || len(data) != 1024 {
		return fmt.Errorf("playback proxy returned HTTP %d and %d bytes", response.StatusCode, len(data))
	}
	return nil
}

func verifyHLS(ctx context.Context, stream media.Stream) error {
	address, close, err := playback.Open(stream.Resource())
	if err != nil {
		return err
	}
	defer close()
	for depth := 0; depth < 4; depth++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return errors.New("invalid HLS address")
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			return errors.New("HLS proxy read failed")
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			return fmt.Errorf("HLS resource returned HTTP %d", response.StatusCode)
		}
		if depth > 0 && len(data) >= 376 && data[0] == 0x47 && data[188] == 0x47 {
			return nil
		}
		if !bytes.HasPrefix(data, []byte("#EXTM3U")) {
			return errors.New("HLS resource was neither a playlist nor a transport stream")
		}
		address = ""
		for line := range strings.SplitSeq(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			next, err := request.URL.Parse(line)
			if err != nil || next.Host != request.URL.Host || next.Scheme != request.URL.Scheme {
				return errors.New("HLS resource did not use the loopback proxy")
			}
			address = next.String()
			break
		}
		if address == "" {
			return errors.New("HLS playlist has no media references")
		}
	}
	return errors.New("no media segment found in the HLS playlists")
}
