/*
Copyright 2026 The Kubernetes Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeCaller records forwarded calls and returns canned results keyed by tool.
type fakeCaller struct {
	mu      sync.Mutex
	calls   []callRecord
	results map[string]any
	err     error
}

type callRecord struct {
	server string
	tool   string
	args   map[string]any
}

func (f *fakeCaller) Call(_ context.Context, server, tool string, args map[string]any) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, callRecord{server: server, tool: tool, args: args})
	if f.err != nil {
		return nil, f.err
	}
	return f.results[tool], nil
}

func runEngine(t *testing.T, caller ToolCaller, servers []string, code string, timeout time.Duration) (any, error) {
	t.Helper()
	e, err := newEngine(caller, servers)
	if err != nil {
		t.Fatalf("newEngine: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return e.Run(ctx, code)
}

func TestForwardAllowListedTool(t *testing.T) {
	caller := &fakeCaller{results: map[string]any{
		"listPods": map[string]any{"count": int64(3)},
	}}

	// Both the dynamic tools.call and the tools.<server>.<tool>() sugar must work.
	res, err := runEngine(t, caller, []string{"k8s"},
		`var a = tools.call('k8s', 'listPods', {ns: 'default'}).count;
		 var b = tools.k8s.listPods({ns: 'default'}).count;
		 return a + b;`, 5*time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := res, int64(6); got != want {
		t.Errorf("result = %v; want %v", got, want)
	}
	if len(caller.calls) != 2 {
		t.Fatalf("caller saw %d calls; want 2", len(caller.calls))
	}
	if caller.calls[0].server != "k8s" || caller.calls[0].tool != "listPods" {
		t.Errorf("first call = %+v; want k8s/listPods", caller.calls[0])
	}
	if caller.calls[0].args["ns"] != "default" {
		t.Errorf("args not forwarded: %+v", caller.calls[0].args)
	}
}

func TestNonAllowListedServerIsScriptError(t *testing.T) {
	caller := &fakeCaller{}
	_, err := runEngine(t, caller, []string{"k8s"},
		`return tools.call('secrets-vault', 'read', {});`, 5*time.Second)

	var re *RunnerError
	if !errors.As(err, &re) || re.Type != ErrorTypeScript {
		t.Fatalf("err = %v; want ScriptError", err)
	}
	if len(caller.calls) != 0 {
		t.Errorf("containment breached: caller saw %d calls; want 0", len(caller.calls))
	}
}

func TestScriptSyntaxErrorIsScriptError(t *testing.T) {
	_, err := runEngine(t, &fakeCaller{}, []string{"k8s"},
		`this is not valid javascript (`, 5*time.Second)

	var re *RunnerError
	if !errors.As(err, &re) || re.Type != ErrorTypeScript {
		t.Fatalf("err = %v; want ScriptError", err)
	}
}

func TestScriptRuntimeErrorIsScriptError(t *testing.T) {
	_, err := runEngine(t, &fakeCaller{}, []string{"k8s"},
		`return undefinedVariable.field;`, 5*time.Second)

	var re *RunnerError
	if !errors.As(err, &re) || re.Type != ErrorTypeScript {
		t.Fatalf("err = %v; want ScriptError", err)
	}
}

func TestTimeoutIsTimeoutError(t *testing.T) {
	_, err := runEngine(t, &fakeCaller{}, []string{"k8s"},
		`while (true) {}`, 100*time.Millisecond)

	var re *RunnerError
	if !errors.As(err, &re) || re.Type != ErrorTypeTimeout {
		t.Fatalf("err = %v; want TimeoutError", err)
	}
}

func TestBackendErrorKeepsType(t *testing.T) {
	caller := &fakeCaller{err: backendError("connection refused")}
	_, err := runEngine(t, caller, []string{"k8s"},
		`return tools.k8s.listPods({});`, 5*time.Second)

	var re *RunnerError
	if !errors.As(err, &re) || re.Type != ErrorTypeBackend {
		t.Fatalf("err = %v; want BackendError", err)
	}
}

func TestOnlyFinalResultReturned(t *testing.T) {
	caller := &fakeCaller{results: map[string]any{
		"get": map[string]any{"value": int64(10)},
	}}
	// Several intermediate calls, but only one value comes back.
	res, err := runEngine(t, caller, []string{"k8s"},
		`var total = 0;
		 for (var i = 0; i < 3; i++) { total += tools.k8s.get({}).value; }
		 return total;`, 5*time.Second)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, want := res, int64(30); got != want {
		t.Errorf("result = %v; want %v", got, want)
	}
	if len(caller.calls) != 3 {
		t.Errorf("caller saw %d calls; want 3", len(caller.calls))
	}
}

func TestRunEmitsSingleResultEnvelope(t *testing.T) {
	var buf bytes.Buffer
	env := runnerEnv{code: "return 1 + 2;", timeout: "5"}
	env.run(context.Background(), &buf)

	var decoded Envelope
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode envelope: %v (%q)", err, buf.String())
	}
	if decoded.Error != nil {
		t.Fatalf("unexpected error envelope: %+v", decoded.Error)
	}
	if got, want := decoded.Result, float64(3); got != want {
		t.Errorf("result = %v; want %v", got, want)
	}
}

func TestRunEmitsErrorEnvelope(t *testing.T) {
	var buf bytes.Buffer
	env := runnerEnv{code: "return tools.call('nope', 'x', {});", timeout: "5"}
	env.run(context.Background(), &buf)

	var decoded Envelope
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("decode envelope: %v (%q)", err, buf.String())
	}
	if decoded.Error == nil || decoded.Error.Type != ErrorTypeScript {
		t.Fatalf("error envelope = %+v; want ScriptError", decoded.Error)
	}
}
