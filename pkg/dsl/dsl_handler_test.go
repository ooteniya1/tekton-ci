package dsl

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/jenkins-x/go-scm/scm/factory"
	"github.com/prometheus/client_golang/prometheus"
	fakeclientset "github.com/tektoncd/pipeline/pkg/client/clientset/versioned/fake"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/bigkevmcd/tekton-ci/pkg/git"
	"github.com/bigkevmcd/tekton-ci/pkg/metrics"
	"github.com/bigkevmcd/tekton-ci/pkg/secrets"
	"github.com/bigkevmcd/tekton-ci/test"
)

const testNS = "testing"

func TestHandlePushEvent(t *testing.T) {
	as := test.MakeAPIServer(t, "/api/v3/repos/Codertocat/Hello-World/contents/.tekton_ci.yaml", "6113728f27ae82c7b1a177c8d03f9e96e0adf246", "testdata/content.json")
	defer as.Close()
	scmClient, err := factory.NewClient("github", as.URL, "", factory.Client(as.Client()))
	if err != nil {
		t.Fatal(err)
	}
	gitClient := git.New(scmClient, secrets.NewMock())
	fakeTektonClient := fakeclientset.NewSimpleClientset()
	cfg := testConfiguration()
	logger := zaptest.NewLogger(t, zaptest.Level(zap.WarnLevel))
	h := New(gitClient, fakeTektonClient, metrics.New(prometheus.NewRegistry()), cfg, testNS, logger.Sugar())
	req := test.MakeHookRequest(t, "../testdata/github_push.json", "push")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	w := rec.Result()
	if w.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want %d: %s", w.StatusCode, http.StatusNotFound, mustReadBody(t, w))
	}
	pr, err := fakeTektonClient.TektonV1beta1().PipelineRuns(testNS).Get("", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if l := len(pr.Spec.PipelineSpec.Tasks); l != 4 {
		t.Fatalf("got %d tasks, want 4", l)
	}
	// check that it picked up the correct source URL and branch from the
	// fixture file.
	want := []string{
		"/ko-app/git-init",
		"-url", "https://github.com/Codertocat/Hello-World.git",
		"-revision", "6113728f27ae82c7b1a177c8d03f9e96e0adf246",
		"-path", "$(workspaces.source.path)",
	}
	if diff := cmp.Diff(want, pr.Spec.PipelineSpec.Tasks[0].TaskSpec.Steps[0].Container.Command); diff != "" {
		t.Fatalf("git command incorrect, diff\n%s", diff)
	}
	prUUID := pr.ObjectMeta.Annotations[hookIDAnnotation]
	if deliveryID := req.Header.Get("X-GitHub-Delivery"); prUUID != deliveryID {
		t.Fatalf("PR UUID got %s, want %s", prUUID, deliveryID)
	}
}

func TestHandlePushEventNoPipeline(t *testing.T) {
	as := test.MakeAPIServer(t, "/api/v3/repos/Codertocat/Hello-World/contents/.tekton_ci.yaml", "6113728f27ae82c7b1a177c8d03f9e96e0adf246", "")
	defer as.Close()
	scmClient, err := factory.NewClient("github", as.URL, "", factory.Client(as.Client()))
	if err != nil {
		t.Fatal(err)
	}
	gitClient := git.New(scmClient, secrets.NewMock())
	fakeTektonClient := fakeclientset.NewSimpleClientset()
	logger := zaptest.NewLogger(t, zaptest.Level(zap.WarnLevel))
	h := New(gitClient, fakeTektonClient, metrics.New(prometheus.NewRegistry()), testConfiguration(), testNS, logger.Sugar())
	req := test.MakeHookRequest(t, "../testdata/github_push.json", "push")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	w := rec.Result()
	if w.StatusCode != http.StatusOK {
		t.Fatalf("got %d, want %d: %s", w.StatusCode, http.StatusOK, mustReadBody(t, w))
	}
	_, err = fakeTektonClient.TektonV1beta1().PipelineRuns(testNS).Get("", metav1.GetOptions{})
	if !errors.IsNotFound(err) {
		t.Fatalf("pipelinerun was created when no pipeline definition exists")
	}
}

func mustReadBody(t *testing.T, req *http.Response) []byte {
	t.Helper()
	b, err := ioutil.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
