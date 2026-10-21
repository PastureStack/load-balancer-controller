package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	SELF       = "self"
	COLLECTION = "collection"
)

var (
	debug             = false
	dialer            = &websocket.Dialer{}
	privateFieldRegex = regexp.MustCompile("^[[:lower:]]")
)

type ClientOpts struct {
	Url       string
	AccessKey string
	SecretKey string
	Timeout   time.Duration
}

type ApiError struct {
	StatusCode int
	Url        string
	Msg        string
	Status     string
	Body       string
}

func (e *ApiError) Error() string {
	return e.Msg
}

func IsNotFound(err error) bool {
	apiError, ok := err.(*ApiError)
	if !ok {
		return false
	}

	return apiError.StatusCode == http.StatusNotFound
}

func newApiError(resp *http.Response, url string) *ApiError {
	contents, err := io.ReadAll(resp.Body)
	var body string
	if err != nil {
		body = "Unreadable body."
	} else {
		body = string(contents)
	}

	data := map[string]interface{}{}
	if json.Unmarshal(contents, &data) == nil {
		delete(data, "id")
		delete(data, "links")
		delete(data, "actions")
		delete(data, "type")
		delete(data, "status")
		buf := &bytes.Buffer{}
		for k, v := range data {
			if v == nil {
				continue
			}
			if buf.Len() > 0 {
				buf.WriteString(", ")
			}
			fmt.Fprintf(buf, "%s=%v", k, v)
		}
		body = buf.String()
	}
	formattedMsg := fmt.Sprintf("Bad response statusCode [%d]. Status [%s]. Body: [%s] from [%s]",
		resp.StatusCode, resp.Status, body, url)
	return &ApiError{
		Url:        url,
		Msg:        formattedMsg,
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Body:       body,
	}
}

func contains(array []string, item string) bool {
	for _, check := range array {
		if check == item {
			return true
		}
	}

	return false
}

func appendFilters(urlString string, filters map[string]interface{}) (string, error) {
	if len(filters) == 0 {
		return urlString, nil
	}

	u, err := url.Parse(urlString)
	if err != nil {
		return "", err
	}

	q := u.Query()
	for k, v := range filters {
		if l, ok := v.([]string); ok {
			for _, v := range l {
				q.Add(k, v)
			}
		} else {
			q.Add(k, fmt.Sprintf("%v", v))
		}
	}

	u.RawQuery = q.Encode()
	return u.String(), nil
}

func NormalizeUrl(existingUrl string) (string, error) {
	u, err := url.Parse(existingUrl)
	if err != nil {
		return "", err
	}

	if u.Path == "" || u.Path == "/" {
		u.Path = "v2-beta"
	} else if u.Path == "/v1" || strings.HasPrefix(u.Path, "/v1/") {
		u.Path = strings.Replace(u.Path, "/v1", "/v2-beta", 1)
	}

	return u.String(), nil
}

func safeURLForLog(value string) string {
	u, err := url.Parse(value)
	if err != nil {
		return "[invalid URL]"
	}
	u.RawQuery = ""
	u.Fragment = ""
	return u.Redacted()
}

// ErrorSummary omits ApiError response bodies and url.Error request URLs while
// retaining their failure category. Callers must still escape log controls.
func ErrorSummary(err error) string {
	if err == nil {
		return ""
	}
	var apiError *ApiError
	if errors.As(err, &apiError) {
		return fmt.Sprintf("HTTP status=%d %s", apiError.StatusCode, apiError.Status)
	}
	var requestError *url.Error
	if errors.As(err, &requestError) {
		return requestError.Op + ": " + ErrorSummary(requestError.Err)
	}
	return err.Error()
}

// This constructor-time value is independent of mutable ClientOpts and of
// every URL subsequently supplied by a control-plane response.
type requestOrigin struct {
	root               url.URL
	scheme, host, port string
}

func originParts(u *url.URL) (scheme, host, port string, err error) {
	scheme = strings.ToLower(u.Scheme)
	host = u.Hostname()
	if ip, parseErr := netip.ParseAddr(host); parseErr == nil {
		host = ip.String()
	} else if !strings.Contains(host, ":") {
		host = strings.ToLower(host)
	} // Preserve case-sensitive IPv6 zone identifiers.
	if (scheme != "http" && scheme != "https") || host == "" || u.Opaque != "" {
		return "", "", "", errors.New("control-plane URL must be absolute HTTP or HTTPS")
	}
	port = u.Port()
	if port == "" {
		if scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	} else {
		number, parseErr := strconv.ParseUint(port, 10, 16)
		if parseErr != nil {
			return "", "", "", errors.New("invalid control-plane URL port")
		}
		port = strconv.FormatUint(number, 10)
	}
	return scheme, host, port, nil
}

func newRequestOrigin(root string) (requestOrigin, error) {
	u, err := url.Parse(root)
	if err != nil {
		return requestOrigin{}, err
	}
	scheme, host, port, err := originParts(u)
	if err != nil {
		return requestOrigin{}, err
	}
	u.Scheme = scheme
	return requestOrigin{root: *u, scheme: scheme, host: host, port: port}, nil
}

func (origin requestOrigin) resolve(value string) (*url.URL, error) {
	if origin.scheme == "" {
		return nil, errors.New("control-plane origin is not initialized")
	}
	u, err := url.Parse(value)
	if err != nil {
		return nil, err
	}
	u = origin.root.ResolveReference(u)
	scheme, host, port, err := originParts(u)
	if err != nil {
		return nil, err
	}
	if scheme != origin.scheme || host != origin.host || port != origin.port {
		return nil, errors.New("cross-origin control-plane URL blocked")
	}
	u.Scheme = scheme
	return u, nil
}

func newHTTPClient(timeout time.Duration, origin requestOrigin) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			req.Header.Del("Authorization")
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			if _, err := origin.resolve(req.URL.String()); err != nil {
				return errors.New("cross-origin redirect blocked")
			}
			if len(via) > 0 {
				req.Header.Set("Authorization", via[0].Header.Get("Authorization"))
			}
			return nil
		},
	}
}

func setupRancherBaseClient(rancherClient *RancherBaseClientImpl, opts *ClientOpts) error {
	var err error
	opts.Url, err = NormalizeUrl(opts.Url)
	if err != nil {
		return err
	}

	if opts.Timeout == 0 {
		opts.Timeout = time.Second * time.Duration(defaultTimeout())
	}
	rancherClient.origin, err = newRequestOrigin(opts.Url)
	if err != nil {
		return err
	}
	rancherClient.Opts = opts
	client := rancherClient.newHttpClient()
	req, err := rancherClient.newRequest("GET", opts.Url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return newApiError(resp, opts.Url)
	}

	schemasUrls := resp.Header.Get("X-API-Schemas")
	if len(schemasUrls) == 0 {
		return errors.New("control-plane response is missing X-API-Schemas")
	}

	if schemasUrls != opts.Url {
		req, err = rancherClient.newRequest("GET", schemasUrls, nil)
		if err != nil {
			return err
		}
		resp, err = client.Do(req)
		if err != nil {
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return newApiError(resp, opts.Url)
		}
	}

	var schemas Schemas
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	err = json.Unmarshal(bytes, &schemas)
	if err != nil {
		return err
	}

	rancherClient.Opts = opts
	rancherClient.Schemas = &schemas

	for _, schema := range schemas.Data {
		rancherClient.Types[schema.Id] = schema
	}

	return nil
}

func NewListOpts() *ListOpts {
	return &ListOpts{
		Filters: map[string]interface{}{},
	}
}

func (rancherClient *RancherBaseClientImpl) newRequest(method, value string, body io.Reader) (*http.Request, error) {
	u, err := rancherClient.origin.resolve(value)
	if err != nil {
		return nil, err
	}
	if rancherClient.Opts == nil {
		return nil, errors.New("control-plane credentials are not initialized")
	}
	req, err := http.NewRequest(method, u.String(), body)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(rancherClient.Opts.AccessKey, rancherClient.Opts.SecretKey)
	return req, nil
}

func (rancherClient *RancherBaseClientImpl) newHttpClient() *http.Client {
	if rancherClient.Opts.Timeout == 0 {
		rancherClient.Opts.Timeout = time.Second * time.Duration(defaultTimeout())
	}
	return newHTTPClient(rancherClient.Opts.Timeout, rancherClient.origin)
}

func (rancherClient *RancherBaseClientImpl) doDelete(url string) error {
	client := rancherClient.newHttpClient()
	req, err := rancherClient.newRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 300 {
		return newApiError(resp, url)
	}

	return nil
}

func (rancherClient *RancherBaseClientImpl) Websocket(value string, headers map[string][]string) (*websocket.Conn, *http.Response, error) {
	// WebSocket URLs use the corresponding HTTP origin. The dialer does not
	// follow redirects; validate before adding any caller or Basic Auth header.
	u, err := url.Parse(value)
	if err != nil {
		return nil, nil, err
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	default:
		return nil, nil, errors.New("control-plane WebSocket URL must use ws or wss")
	}
	if _, err := rancherClient.origin.resolve(u.String()); err != nil {
		return nil, nil, err
	}
	httpHeaders := http.Header{}
	for k, v := range headers {
		if strings.EqualFold(k, "Host") {
			if len(v) != 1 {
				return nil, nil, errors.New("invalid control-plane WebSocket Host header")
			}
			authority := *u
			authority.Host = v[0]
			if _, err := rancherClient.origin.resolve(authority.String()); err != nil {
				return nil, nil, err
			}
		}
		if strings.EqualFold(k, "Authorization") && rancherClient.Opts != nil {
			continue
		}
		httpHeaders[k] = v
	}

	if rancherClient.Opts != nil {
		s := rancherClient.Opts.AccessKey + ":" + rancherClient.Opts.SecretKey
		httpHeaders.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(s)))
	}

	return dialer.Dial(value, http.Header(httpHeaders))
}

func (rancherClient *RancherBaseClientImpl) doGet(url string, opts *ListOpts, respObject interface{}) error {
	if opts == nil {
		opts = NewListOpts()
	}
	url, err := appendFilters(url, opts.Filters)
	if err != nil {
		return err
	}

	if debug {
		fmt.Println("GET " + safeURLForLog(url))
	}

	client := rancherClient.newHttpClient()
	req, err := rancherClient.newRequest("GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return newApiError(resp, url)
	}

	byteContent, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if debug {
		fmt.Printf("Response <= %d bytes\n", len(byteContent))
	}

	if err := json.Unmarshal(byteContent, respObject); err != nil {
		return fmt.Errorf("failed to parse response %q: %w", byteContent, err)
	}

	return nil
}

func (rancherClient *RancherBaseClientImpl) List(schemaType string, opts *ListOpts, respObject interface{}) error {
	return rancherClient.doList(schemaType, opts, respObject)
}

func (rancherClient *RancherBaseClientImpl) doList(schemaType string, opts *ListOpts, respObject interface{}) error {
	schema, ok := rancherClient.Types[schemaType]
	if !ok {
		return errors.New("Unknown schema type [" + schemaType + "]")
	}

	if !contains(schema.CollectionMethods, "GET") {
		return errors.New("Resource type [" + schemaType + "] is not listable")
	}

	collectionUrl, ok := schema.Links[COLLECTION]
	if !ok {
		return errors.New("Failed to find collection URL for [" + schemaType + "]")
	}

	return rancherClient.doGet(collectionUrl, opts, respObject)
}

func (rancherClient *RancherBaseClientImpl) doNext(nextUrl string, respObject interface{}) error {
	return rancherClient.doGet(nextUrl, nil, respObject)
}

func (rancherClient *RancherBaseClientImpl) Post(url string, createObj interface{}, respObject interface{}) error {
	return rancherClient.doModify("POST", url, createObj, respObject)
}

func (rancherClient *RancherBaseClientImpl) GetLink(resource Resource, link string, respObject interface{}) error {
	url := resource.Links[link]
	if url == "" {
		return fmt.Errorf("Failed to find link: %s", link)
	}

	return rancherClient.doGet(url, &ListOpts{}, respObject)
}

func (rancherClient *RancherBaseClientImpl) doModify(method string, url string, createObj interface{}, respObject interface{}) error {
	bodyContent, err := json.Marshal(createObj)
	if err != nil {
		return err
	}

	if debug {
		fmt.Printf("%s %s request=%d bytes\n", method, safeURLForLog(url), len(bodyContent))
	}

	client := rancherClient.newHttpClient()
	req, err := rancherClient.newRequest(method, url, bytes.NewBuffer(bodyContent))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return newApiError(resp, url)
	}

	byteContent, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if len(byteContent) > 0 {
		if debug {
			fmt.Printf("Response <= %d bytes\n", len(byteContent))
		}
		return json.Unmarshal(byteContent, respObject)
	}

	return nil
}

func (rancherClient *RancherBaseClientImpl) Create(schemaType string, createObj interface{}, respObject interface{}) error {
	return rancherClient.doCreate(schemaType, createObj, respObject)
}

func (rancherClient *RancherBaseClientImpl) doCreate(schemaType string, createObj interface{}, respObject interface{}) error {
	if createObj == nil {
		createObj = map[string]string{}
	}
	if respObject == nil {
		respObject = &map[string]interface{}{}
	}
	schema, ok := rancherClient.Types[schemaType]
	if !ok {
		return errors.New("Unknown schema type [" + schemaType + "]")
	}

	if !contains(schema.CollectionMethods, "POST") {
		return errors.New("Resource type [" + schemaType + "] is not creatable")
	}

	var collectionUrl string
	collectionUrl, ok = schema.Links[COLLECTION]
	if !ok {
		// return errors.New("Failed to find collection URL for [" + schemaType + "]")
		// This is a hack to address https://github.com/rancher/cattle/issues/254
		re := regexp.MustCompile("schemas.*")
		collectionUrl = re.ReplaceAllString(schema.Links[SELF], schema.PluralName)
	}

	return rancherClient.doModify("POST", collectionUrl, createObj, respObject)
}

func (rancherClient *RancherBaseClientImpl) Update(schemaType string, existing *Resource, updates interface{}, respObject interface{}) error {
	return rancherClient.doUpdate(schemaType, existing, updates, respObject)
}

func (rancherClient *RancherBaseClientImpl) doUpdate(schemaType string, existing *Resource, updates interface{}, respObject interface{}) error {
	if existing == nil {
		return errors.New("Existing object is nil")
	}

	selfUrl, ok := existing.Links[SELF]
	if !ok {
		return fmt.Errorf("failed to find self URL of [%v]", existing)
	}

	if updates == nil {
		updates = map[string]string{}
	}

	if respObject == nil {
		respObject = &map[string]interface{}{}
	}

	schema, ok := rancherClient.Types[schemaType]
	if !ok {
		return errors.New("Unknown schema type [" + schemaType + "]")
	}

	if !contains(schema.ResourceMethods, "PUT") {
		return errors.New("Resource type [" + schemaType + "] is not updatable")
	}

	return rancherClient.doModify("PUT", selfUrl, updates, respObject)
}

func (rancherClient *RancherBaseClientImpl) ById(schemaType string, id string, respObject interface{}) error {
	return rancherClient.doById(schemaType, id, respObject)
}

func (rancherClient *RancherBaseClientImpl) doById(schemaType string, id string, respObject interface{}) error {
	schema, ok := rancherClient.Types[schemaType]
	if !ok {
		return errors.New("Unknown schema type [" + schemaType + "]")
	}

	if !contains(schema.ResourceMethods, "GET") {
		return errors.New("Resource type [" + schemaType + "] can not be looked up by ID")
	}

	collectionUrl, ok := schema.Links[COLLECTION]
	if !ok {
		return errors.New("Failed to find collection URL for [" + schemaType + "]")
	}

	err := rancherClient.doGet(collectionUrl+"/"+id, nil, respObject)
	//TODO check for 404 and return nil, nil
	return err
}

func (rancherClient *RancherBaseClientImpl) Delete(existing *Resource) error {
	if existing == nil {
		return nil
	}
	return rancherClient.doResourceDelete(existing.Type, existing)
}

func (rancherClient *RancherBaseClientImpl) doResourceDelete(schemaType string, existing *Resource) error {
	schema, ok := rancherClient.Types[schemaType]
	if !ok {
		return errors.New("Unknown schema type [" + schemaType + "]")
	}

	if !contains(schema.ResourceMethods, "DELETE") {
		return errors.New("Resource type [" + schemaType + "] can not be deleted")
	}

	selfUrl, ok := existing.Links[SELF]
	if !ok {
		return fmt.Errorf("failed to find self URL of [%v]", existing)
	}

	return rancherClient.doDelete(selfUrl)
}

func (rancherClient *RancherBaseClientImpl) Reload(existing *Resource, output interface{}) error {
	selfUrl, ok := existing.Links[SELF]
	if !ok {
		return fmt.Errorf("failed to find self URL of [%v]", existing)
	}

	return rancherClient.doGet(selfUrl, NewListOpts(), output)
}

func (rancherClient *RancherBaseClientImpl) Action(schemaType string, action string,
	existing *Resource, inputObject, respObject interface{}) error {
	return rancherClient.doAction(schemaType, action, existing, inputObject, respObject)
}

func (rancherClient *RancherBaseClientImpl) doAction(schemaType string, action string,
	existing *Resource, inputObject, respObject interface{}) error {

	if existing == nil {
		return errors.New("Existing object is nil")
	}

	actionUrl, ok := existing.Actions[action]
	if !ok {
		return fmt.Errorf("action [%v] not available on [%v]", action, existing)
	}

	_, ok = rancherClient.Types[schemaType]
	if !ok {
		return errors.New("Unknown schema type [" + schemaType + "]")
	}

	var input io.Reader

	if inputObject != nil {
		bodyContent, err := json.Marshal(inputObject)
		if err != nil {
			return err
		}
		if debug {
			fmt.Printf("Action request => %d bytes\n", len(bodyContent))
		}
		input = bytes.NewBuffer(bodyContent)
	}

	client := rancherClient.newHttpClient()
	req, err := rancherClient.newRequest("POST", actionUrl, input)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", "0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return newApiError(resp, actionUrl)
	}

	byteContent, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if debug {
		fmt.Printf("Response <= %d bytes\n", len(byteContent))
	}

	return json.Unmarshal(byteContent, respObject)
}

func (rancherClient *RancherBaseClientImpl) GetOpts() *ClientOpts {
	return rancherClient.Opts
}

func (rancherClient *RancherBaseClientImpl) GetSchemas() *Schemas {
	return rancherClient.Schemas
}

func (rancherClient *RancherBaseClientImpl) GetTypes() map[string]Schema {
	return rancherClient.Types
}

func init() {
	debug = os.Getenv("RANCHER_CLIENT_DEBUG") == "true"
	if debug {
		fmt.Println("Rancher client debug on")
	}
}

func defaultTimeout() int {
	defaultTimeout, _ := strconv.Atoi(os.Getenv("RANCHER_CLIENT_TIMEOUT"))
	if defaultTimeout == 0 {
		defaultTimeout = 10
	}
	return defaultTimeout
}
