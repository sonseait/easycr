package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func command(t *testing.T, dir string, env []string, input string, args ...string) ([]byte, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, args[0], args[1:]...)
	c.Dir = dir
	c.Env = append(os.Environ(), env...)
	c.Stdin = strings.NewReader(input)
	return c.CombinedOutput()
}
func requireCommand(t *testing.T, dir string, env []string, input string, args ...string) []byte {
	t.Helper()
	b, e := command(t, dir, env, input, args...)
	if e != nil { // input may contain a generated credential: never print it
		out := string(b)
		if input != "" {
			out = strings.ReplaceAll(out, strings.TrimSpace(input), "[REDACTED]")
		}
		t.Fatalf("%s failed: %v\n%s", strings.Join(args, " "), e, out)
	}
	return b
}

type logs struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *logs) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.Write(p)
}
func (l *logs) text() string { l.mu.Lock(); defer l.mu.Unlock(); return l.buffer.String() }

type process struct {
	cmd     *exec.Cmd
	log     *logs
	done    chan error
	stopped bool
}

func start(t *testing.T, binary, dir, host string) *process {
	t.Helper()
	l := &logs{}
	c := exec.Command(binary, "serve")

	c.Dir = filepath.Dir(dir)
	c.Stdout = l
	c.Stderr = l
	if e := c.Start(); e != nil {
		t.Fatal(e)
	}
	p := &process{cmd: c, log: l, done: make(chan error, 1)}
	go func() { p.done <- c.Wait() }()
	t.Cleanup(func() { p.stop(t) })
	return p
}
func (p *process) stop(t *testing.T) {
	t.Helper()
	if p.stopped {
		return
	}
	p.stopped = true
	_ = p.cmd.Process.Signal(os.Interrupt)
	select {
	case e := <-p.done:
		if e != nil {
			t.Errorf("server shutdown: %v", e)
		}
	case <-time.After(10 * time.Second):
		_ = p.cmd.Process.Kill()
		<-p.done
		t.Error("server did not shut down")
	}
}
func bootstrap(t *testing.T, p *process) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(p.log.text(), "\n") {
			if strings.HasPrefix(line, "bootstrap_admin_password password=") {
				return strings.TrimPrefix(line, "bootstrap_admin_password password=")
			}
		}
		select {
		case <-p.done:
			p.stopped = true
			t.Fatal("server exited before bootstrap; logs withheld to protect credentials")
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no password before HTTP requests")
	return ""
}
func healthy(t *testing.T, base string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		r, e := client.Get(base + "/healthz")
		if e == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server health timed out")
}

type admin struct {
	t          *testing.T
	base, csrf string
	client     *http.Client
}

func (a *admin) request(method, path string, body any, want int) []byte {
	a.t.Helper()
	var b []byte
	if body != nil {
		var e error
		b, e = json.Marshal(body)
		if e != nil {
			a.t.Fatal(e)
		}
	}
	r, e := http.NewRequest(method, a.base+path, bytes.NewReader(b))
	if e != nil {
		a.t.Fatal(e)
	}
	r.Header.Set("Origin", a.base)
	r.Header.Set("X-CSRF-Token", a.csrf)
	r.Header.Set("Content-Type", "application/json")
	response, e := a.client.Do(r)
	if e != nil {
		a.t.Fatal(e)
	}
	defer response.Body.Close()
	out, e := io.ReadAll(response.Body)
	if e != nil {
		a.t.Fatal(e)
	}
	if response.StatusCode != want {
		a.t.Fatalf("%s %s status %d want %d (response withheld)", method, path, response.StatusCode, want)
	}
	return out
}
func (a *admin) login(password string) {
	a.t.Helper()
	b := a.request("POST", "/api/login", map[string]string{"password": password}, 200)
	var v struct {
		CSRF string `json:"csrfToken"`
	}
	if e := json.Unmarshal(b, &v); e != nil {
		a.t.Fatal(e)
	}
	a.csrf = v.CSRF
}
func (a *admin) token(repo, permission string) (string, string) {
	a.t.Helper()
	b := a.request("POST", "/api/tokens", map[string]any{"name": "e2e", "grants": []map[string]string{{"repository": repo, "permission": permission}}}, 201)
	var v struct {
		Token struct {
			ID string `json:"id"`
		} `json:"token"`
		Secret string `json:"secret"`
	}
	if e := json.Unmarshal(b, &v); e != nil {
		a.t.Fatal(e)
	}
	if v.Secret == "" {
		a.t.Fatal("missing token")
	}
	return v.Token.ID, v.Secret
}
func TestDockerWorkflow(t *testing.T) {
	if os.Getenv("EASYCR_E2E") != "1" {
		t.Skip("opt in with EASYCR_E2E=1; requires a running local Docker daemon")
	}
	// An opt-in run must fail, never skip or report success, without Docker.
	requireCommand(t, "", nil, "", "docker", "info", "--format", "{{.ServerVersion}}")
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	dir := t.TempDir()
	binary := filepath.Join(dir, "easycr")
	requireCommand(t, root, nil, "", "go", "build", "-o", binary, "./cmd/easycr")
	l, e := net.Listen("tcp", "127.0.0.1:5000")
	if e != nil {
		t.Fatal(e)
	}
	host := fmt.Sprintf("localhost:%d", l.Addr().(*net.TCPAddr).Port)
	l.Close()
	base := "http://" + host
	data := filepath.Join(dir, "data")
	p := start(t, binary, data, host)
	password := bootstrap(t, p) // explicitly before the first health, UI, or API request
	healthy(t, base)
	jar, _ := cookiejar.New(nil)
	a := admin{t: t, base: base, client: &http.Client{Jar: jar, Timeout: 15 * time.Second}}
	a.request("GET", "/", nil, 200)
	a.request("GET", "/", nil, 200)
	if strings.Count(p.log.text(), "bootstrap_admin_password") != 1 {
		t.Fatal("bootstrap repeated by GET")
	}
	a.login(password)
	id, writer := a.token("team/app", "write")
	_, reader := a.token("team/app", "read")
	_, global := a.token("*", "write")
	dockerConfig := filepath.Join(dir, "docker")
	if e = os.MkdirAll(dockerConfig, 0700); e != nil {
		t.Fatal(e)
	}
	env := []string{"DOCKER_CONFIG=" + dockerConfig}
	login := func(secret string) {
		t.Helper()
		requireCommand(t, root, env, secret+"\n", "docker", "login", host, "--username", "token", "--password-stdin")
	}
	fixture := filepath.Join(dir, "fixture")
	os.MkdirAll(fixture, 0700)
	os.WriteFile(filepath.Join(fixture, "hello.txt"), []byte("EasyCR Docker integration\n"), 0600)
	os.WriteFile(filepath.Join(fixture, "Dockerfile"), []byte("FROM scratch\nCOPY hello.txt /hello.txt\n"), 0600)
	image := host + "/team/app:v1"
	foreign := host + "/foreign/app:v1"
	t.Cleanup(func() { _, _ = command(t, root, env, "", "docker", "image", "rm", "-f", image, foreign) })
	requireCommand(t, root, env, "", "docker", "build", "-t", image, fixture)
	login(writer)
	requireCommand(t, root, env, "", "docker", "push", image)
	a.request("GET", "/api/tags?repository=team%2Fapp", nil, 200)
	// Remove the local tag before pull so this exercises real daemon download.
	requireCommand(t, root, env, "", "docker", "image", "rm", image)
	requireCommand(t, root, env, "", "docker", "pull", image)
	login(reader)
	requireCommand(t, root, env, "", "docker", "image", "rm", image)
	requireCommand(t, root, env, "", "docker", "pull", image)
	if _, e = command(t, root, env, "", "docker", "push", image); e == nil {
		t.Fatal("read token pushed")
	}
	requireCommand(t, root, env, "", "docker", "tag", image, foreign)
	login(writer)
	if _, e = command(t, root, env, "", "docker", "push", foreign); e == nil {
		t.Fatal("scoped token pushed foreign repository")
	}
	login(global)
	requireCommand(t, root, env, "", "docker", "push", foreign)
	p.stop(t)
	p = start(t, binary, data, host)
	healthy(t, base)
	if strings.Contains(p.log.text(), "bootstrap_admin_password") {
		t.Fatal("restart generated a password")
	}
	a.login(password)
	login(writer)
	requireCommand(t, root, env, "", "docker", "image", "rm", image)
	requireCommand(t, root, env, "", "docker", "pull", image)
	// Cached Docker credentials must fail after revocation on the next request.
	a.request("DELETE", "/api/tokens/"+id, nil, 204)
	if _, e = command(t, root, env, "", "docker", "pull", image); e == nil {
		t.Fatal("revoked cached token pulled")
	}
	p.stop(t)
	requireCommand(t, dir, nil, "", binary, "gc-dry-run")
	requireCommand(t, dir, nil, "", binary, "gc")
	p = start(t, binary, data, host)
	healthy(t, base)
	login(global)
	requireCommand(t, root, env, "", "docker", "image", "rm", image)
	requireCommand(t, root, env, "", "docker", "pull", image)
}

func TestContainerDeployment(t *testing.T) {
	if os.Getenv("EASYCR_E2E") != "1" {
		t.Skip("opt in with EASYCR_E2E=1; requires Docker")
	}
	requireCommand(t, "", nil, "", "docker", "info", "--format", "{{.ServerVersion}}")
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../.."))
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	image := "easycr-e2e:" + suffix
	container := "easycr-e2e-" + suffix
	volume := container + "-data"
	t.Cleanup(func() {
		_, _ = command(t, root, nil, "", "docker", "rm", "-f", container)
		_, _ = command(t, root, nil, "", "docker", "volume", "rm", volume)
		_, _ = command(t, root, nil, "", "docker", "image", "rm", image)
	})
	requireCommand(t, root, nil, "", "docker", "build", "-t", image, ".")
	requireCommand(t, root, nil, "", "docker", "volume", "create", volume)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	host := fmt.Sprintf("localhost:%d", port)
	base := "http://" + host
	requireCommand(t, root, nil, "", "docker", "run", "-d", "--name", container, "-p", fmt.Sprintf("127.0.0.1:%d:5000", port), "-v", volume+":/data", image)
	// Capture the event from logs before sending a single HTTP request.
	password := ""
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		b := requireCommand(t, root, nil, "", "docker", "logs", container)
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(line, "bootstrap_admin_password password=") {
				password = strings.TrimPrefix(line, "bootstrap_admin_password password=")
			}
		}
		if password != "" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if password == "" {
		t.Fatal("container did not bootstrap")
	}
	healthy(t, base)
	user := strings.TrimSpace(string(requireCommand(t, root, nil, "", "docker", "inspect", "--format", "{{.Config.User}}", container)))
	if user != "65532:65532" {
		t.Fatal("container is not configured nonroot")
	}
	jar, _ := cookiejar.New(nil)
	a := admin{t: t, base: base, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
	a.login(password)
	a.token("*", "read")
	if _, e = command(t, root, nil, "", "docker", "run", "--rm", "-v", volume+":/data", image, "gc-dry-run"); e == nil {
		t.Fatal("maintenance ignored running container lock")
	}
	requireCommand(t, root, nil, "", "docker", "restart", container)
	healthy(t, base)
	a.login(password)
	b := a.request("GET", "/api/tokens", nil, 200)
	if !bytes.Contains(b, []byte(`"name":"e2e"`)) {
		t.Fatal("token lost after container restart")
	}
	b = requireCommand(t, root, nil, "", "docker", "logs", container)
	if strings.Count(string(b), "bootstrap_admin_password") != 1 {
		t.Fatal("container restart bootstrapped again")
	}
	requireCommand(t, root, nil, "", "docker", "stop", container)
	requireCommand(t, root, nil, "", "docker", "run", "--rm", "-v", volume+":/data", image, "gc-dry-run")
}
