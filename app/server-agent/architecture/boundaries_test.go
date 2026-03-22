package architecture_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIntentHandlersAvoidStoreAndDomainImports(t *testing.T) {
	for _, relPath := range []string{
		"internal/accountmanager/http_account.go",
		"internal/accountmanager/http_account_creation.go",
		"internal/accountmanager/http_login_run.go",
		"internal/accountmanager/http_persona.go",
		"internal/campaigns/http_post_campaign.go",
		"internal/handler/projection_stream.go",
	} {
		assertNoImports(t, relPath, []string{
			"/internal/store",
			"/internal/domain",
		})
	}
}

func TestDeviceControlPackageAvoidsUsecaseAndHandlerImports(t *testing.T) {
	for _, relPath := range []string{
		"internal/devicectrl/http_device.go",
		"internal/devicectrl/recording.go",
		"internal/devicectrl/llm_recorder.go",
		"internal/devicectrl/adb_ws.go",
		"internal/devicectrl/device_binding_manager.go",
		"internal/devicectrl/accessibility_auto_enabler.go",
		"internal/devicectrl/adb_shell_runner.go",
		"internal/devicectrl/agent_lifecycle.go",
	} {
		assertNoImports(t, relPath, []string{
			"/internal/appport",
			"/internal/handler",
		})
	}
}

func TestIntentUsecasesAvoidHandlerAndTransportImports(t *testing.T) {
	for _, relPath := range []string{
		"internal/accountmanager/service.go",
		"internal/accountmanager/projected_store.go",
		"internal/accountmanager/account_creation_runner.go",
		"internal/accountmanager/login_run_runner.go",
		"internal/campaigns/service.go",
		"internal/campaigns/post_runner.go",
	} {
		assertNoImports(t, relPath, []string{
			"/internal/handler",
			"/internal/transport",
		})
	}
}

func TestWorkflowRuntimePackageAvoidsHandlerAndTransportImports(t *testing.T) {
	for _, relPath := range []string{
		"internal/workflowruntime/ports.go",
		"internal/workflowruntime/device_assigner.go",
		"internal/workflowruntime/task_control.go",
		"internal/workflowruntime/runtime_recovery.go",
		"internal/workflowruntime/task_poller.go",
	} {
		assertNoImports(t, relPath, []string{
			"/internal/handler",
			"/internal/transport",
		})
	}
}

func TestEventingPackageAvoidsHandlerAndTransportImports(t *testing.T) {
	for _, relPath := range []string{
		"internal/eventing/control.go",
		"internal/eventing/ingestion.go",
	} {
		assertNoImports(t, relPath, []string{
			"/internal/handler",
			"/internal/transport",
		})
	}
}

func TestProjectionPackageAvoidsTransportAndUsecaseImports(t *testing.T) {
	for _, relPath := range []string{
		"internal/projection/event.go",
		"internal/projection/hub.go",
	} {
		assertNoImports(t, relPath, []string{
			"/internal/handler",
			"/internal/transport",
			"/internal/appport",
		})
	}
}

func assertNoImports(t *testing.T, relPath string, forbidden []string) {
	t.Helper()

	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	moduleRoot := filepath.Dir(filepath.Dir(thisFile))
	absPath := filepath.Join(moduleRoot, relPath)

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, absPath, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parse imports for %s: %v", relPath, err)
	}

	for _, imp := range file.Imports {
		pathValue := strings.Trim(imp.Path.Value, `"`)
		for _, blocked := range forbidden {
			if strings.Contains(pathValue, blocked) {
				t.Fatalf("%s imports forbidden dependency %s", relPath, pathValue)
			}
		}
	}
}
