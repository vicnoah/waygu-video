package casting

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	transportType  = "urn:schemas-upnp-org:service:AVTransport:"
	connectionType = "urn:schemas-upnp-org:service:ConnectionManager:"
	renderingType  = "urn:schemas-upnp-org:service:RenderingControl:"
)

type actionError struct{ Code int }

func (err actionError) Error() string { return fmt.Sprintf("DLNA action failed (%d)", err.Code) }

type argument struct{ name, value string }

var instance = argument{"InstanceID", "0"}
var master = argument{"Channel", "Master"}

func (s service) action(ctx context.Context, name string, args ...argument) (map[string]string, error) {
	if s.control == nil {
		return nil, ErrDevice
	}
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&body, `<u:%s xmlns:u="%s">`, name, s.kind)
	for _, arg := range args {
		fmt.Fprintf(&body, "<%s>", arg.name)
		_ = xml.EscapeText(&body, []byte(arg.value))
		fmt.Fprintf(&body, "</%s>", arg.name)
	}
	fmt.Fprintf(&body, "</u:%s></s:Body></s:Envelope>", name)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.control.String(), strings.NewReader(body.String()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("SOAPAction", `"`+s.kind+"#"+name+`"`)
	resp, err := lanClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var answer struct {
		Body struct {
			Fault struct {
				Code int `xml:"detail>UPnPError>errorCode"`
			} `xml:"Fault"`
			Response struct {
				XMLName xml.Name
				Fields  []struct {
					XMLName xml.Name
					Value   string `xml:",chardata"`
				} `xml:",any"`
			} `xml:",any"`
		} `xml:"Body"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 128<<10)).Decode(&answer); err != nil {
		return nil, err
	}
	if answer.Body.Fault.Code != 0 {
		return nil, actionError{answer.Body.Fault.Code}
	}
	if resp.StatusCode != http.StatusOK || answer.Body.Response.XMLName.Local != name+"Response" {
		return nil, errors.New("invalid DLNA response")
	}
	values := make(map[string]string)
	for _, field := range answer.Body.Response.Fields {
		values[field.XMLName.Local] = strings.TrimSpace(field.Value)
	}
	return values, nil
}

// Formats holds the receiver's HTTP protocol declarations.
type Formats []string

func (r Renderer) Formats(ctx context.Context) (Formats, error) {
	if r.connection.control == nil {
		return nil, nil
	}
	answer, err := r.connection.action(ctx, "GetProtocolInfo")
	if err != nil {
		return nil, err
	}
	var formats Formats
	for _, value := range strings.Split(answer["Sink"], ",") {
		fields := strings.SplitN(strings.ToLower(strings.TrimSpace(value)), ":", 4)
		if len(fields) == 4 && (fields[0] == "http-get" || fields[0] == "*") && fields[2] != "" {
			formats = append(formats, fields[2])
		}
	}
	return formats, nil
}

func (formats Formats) Supports(mime string) bool {
	mime = strings.ToLower(mime)
	category, _, _ := strings.Cut(mime, "/")
	for _, value := range formats {
		if value == mime || value == "*" || value == "*/*" || value == category+"/*" {
			return true
		}
	}
	return false
}

func (r Renderer) Load(ctx context.Context, address, mime, title string) error {
	var metadata strings.Builder
	metadata.WriteString(`<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item id="1" parentID="0" restricted="1"><dc:title>`)
	_ = xml.EscapeText(&metadata, []byte(title))
	metadata.WriteString(`</dc:title><upnp:class>object.item.videoItem</upnp:class><res protocolInfo="http-get:*:`)
	_ = xml.EscapeText(&metadata, []byte(mime))
	metadata.WriteString(`:*">`)
	_ = xml.EscapeText(&metadata, []byte(address))
	metadata.WriteString("</res></item></DIDL-Lite>")
	_, err := r.transport.action(ctx, "SetAVTransportURI", instance,
		argument{"CurrentURI", address}, argument{"CurrentURIMetaData", metadata.String()})
	if failure, ok := errors.AsType[actionError](err); ok && (failure.Code == 714 || failure.Code == 704) {
		return fmt.Errorf("%w: %v", ErrFormat, err)
	}
	return err
}

func (r Renderer) Play(ctx context.Context, playing bool) error {
	if playing {
		_, err := r.transport.action(ctx, "Play", instance, argument{"Speed", "1"})
		return err
	}
	_, err := r.transport.action(ctx, "Pause", instance)
	return err
}

func (r Renderer) Stop(ctx context.Context) error {
	_, err := r.transport.action(ctx, "Stop", instance)
	return err
}

func (r Renderer) Seek(ctx context.Context, at time.Duration) error {
	seconds := int64(max(0, at) / time.Second)
	clock := fmt.Sprintf("%02d:%02d:%02d", seconds/3600, (seconds/60)%60, seconds%60)
	_, err := r.transport.action(ctx, "Seek", instance, argument{"Unit", "REL_TIME"}, argument{"Target", clock})
	return err
}

func (r Renderer) HasVolume() bool { return r.rendering.control != nil }

func (r Renderer) SetVolume(ctx context.Context, volume int) error {
	_, err := r.rendering.action(ctx, "SetVolume", instance, master, argument{"DesiredVolume", strconv.Itoa(min(100, max(0, volume)))})
	return err
}

func (r Renderer) Volume(ctx context.Context) (int, error) {
	values, err := r.rendering.action(ctx, "GetVolume", instance, master)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(values["CurrentVolume"])
}

type Position struct {
	URI          string
	At, Duration time.Duration
	Available    bool
}

func (r Renderer) Position(ctx context.Context) (Position, error) {
	values, err := r.transport.action(ctx, "GetPositionInfo", instance)
	at, available := relativeTime(values["RelTime"])
	duration, _ := relativeTime(values["TrackDuration"])
	uri := values["TrackURI"]
	if uri == "NOT_IMPLEMENTED" {
		uri = ""
	}
	return Position{URI: uri, At: at, Duration: duration, Available: available}, err
}

func (r Renderer) Status(ctx context.Context) (string, error) {
	values, err := r.transport.action(ctx, "GetTransportInfo", instance)
	if err == nil && values["CurrentTransportState"] == "" {
		err = errors.New("the receiver omitted playback status")
	}
	if err == nil && values["CurrentTransportStatus"] == "ERROR_OCCURRED" {
		err = errors.New("receiver playback failed")
	}
	return values["CurrentTransportState"], err
}

func relativeTime(value string) (time.Duration, bool) {
	items := strings.Split(value, ":")
	if len(items) != 3 {
		return 0, false
	}
	h, hErr := strconv.ParseUint(items[0], 10, 20)
	m, mErr := strconv.ParseUint(items[1], 10, 6)
	s, sErr := time.ParseDuration(items[2] + "s")
	if hErr != nil || mErr != nil || sErr != nil || m >= 60 || s < 0 || s >= time.Minute {
		return 0, false
	}
	return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + s, true
}
