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
	"context"
	"errors"
	"time"

	"github.com/dop251/goja"
)

// bootstrap wraps every allow-listed server in a Proxy so that
// tools.<server>.<tool>(args) resolves to tools.call(server, tool, args). The
// dynamic shape means no per-tool code generation is needed (MVP contract).
const bootstrap = `
(function (servers) {
  servers.forEach(function (s) {
    tools[s] = new Proxy({}, {
      get: function (_target, tool) {
        if (typeof tool !== 'string') {
          return undefined;
        }
        return function (args) {
          return tools.call(s, tool, args || {});
        };
      }
    });
  });
})(__servers);
`

// Engine runs a submitted script in an embedded JavaScript runtime, exposing
// exactly one host capability - forwarding an MCP tool call - and enforcing the
// toolset allow-list (containment, FR-005). It has no filesystem, process, or
// network access beyond that single capability.
type Engine struct {
	vm      *goja.Runtime
	caller  ToolCaller
	allowed map[string]struct{}

	// ctx and hostErr carry per-run state. ctx bounds host tool calls; hostErr
	// records a typed failure raised from a host call so Run can surface it with
	// its original type after the script aborts.
	ctx     context.Context
	hostErr *RunnerError
}

// newEngine builds an engine whose script may reach only the named servers. The
// caller performs the actual MCP forwarding; the engine performs containment.
func newEngine(caller ToolCaller, servers []string) (*Engine, error) {
	e := &Engine{
		vm:      goja.New(),
		caller:  caller,
		allowed: make(map[string]struct{}, len(servers)),
	}
	for _, s := range servers {
		e.allowed[s] = struct{}{}
	}

	tools := e.vm.NewObject()
	if err := tools.Set("call", e.hostCall); err != nil {
		return nil, err
	}
	if err := e.vm.Set("tools", tools); err != nil {
		return nil, err
	}
	if err := e.vm.Set("__servers", servers); err != nil {
		return nil, err
	}
	if _, err := e.vm.RunString(bootstrap); err != nil {
		return nil, err
	}
	// The bootstrap plumbing is internal; hide it from the script.
	if err := e.vm.Set("__servers", goja.Undefined()); err != nil {
		return nil, err
	}
	return e, nil
}

// Run executes the code and returns its final value. Top-level return is allowed
// (the code runs as an IIFE body). ctx bounds the run: on deadline the result is
// a TimeoutError; a containment or backend failure keeps its type; any other
// script failure is a ScriptError.
func (e *Engine) Run(ctx context.Context, code string) (any, error) {
	e.ctx = ctx
	e.hostErr = nil

	if deadline, ok := ctx.Deadline(); ok {
		timer := time.AfterFunc(time.Until(deadline), func() {
			e.vm.Interrupt("execution timed out")
		})
		defer timer.Stop()
	}

	val, err := e.vm.RunString("(function () {\n" + code + "\n})()")
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, timeoutError("execution exceeded the time budget")
		}
		if e.hostErr != nil {
			return nil, e.hostErr
		}
		if _, ok := errors.AsType[*goja.InterruptedError](err); ok {
			return nil, timeoutError("execution exceeded the time budget")
		}
		return nil, scriptError("%v", err)
	}

	if val == nil || goja.IsUndefined(val) || goja.IsNull(val) {
		return nil, nil
	}
	return val.Export(), nil
}

// hostCall implements tools.call(server, tool, args). It enforces containment,
// forwards to the ToolCaller, and converts the result back to a script value. A
// containment or backend failure aborts the script via a JS throw while
// recording the typed error in hostErr for Run to return.
func (e *Engine) hostCall(call goja.FunctionCall) goja.Value {
	server := call.Argument(0).String()
	tool := call.Argument(1).String()

	var args map[string]any
	if a := call.Argument(2); a != nil && !goja.IsUndefined(a) && !goja.IsNull(a) {
		if m, ok := a.Export().(map[string]any); ok {
			args = m
		}
	}

	if _, ok := e.allowed[server]; !ok {
		e.fail(scriptError("server %q is not in this toolset's allow-list", server))
	}

	result, err := e.caller.Call(e.ctx, server, tool, args)
	if err != nil {
		if re, ok := errors.AsType[*RunnerError](err); ok {
			e.fail(re)
		}
		e.fail(backendError("%v", err))
	}
	return e.vm.ToValue(result)
}

// fail records the typed error and throws a JS exception to abort the script.
func (e *Engine) fail(re *RunnerError) {
	e.hostErr = re
	panic(e.vm.ToValue(re.Message))
}
