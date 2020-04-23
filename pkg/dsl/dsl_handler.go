package dsl

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/jenkins-x/go-scm/scm"
	pipelineclientset "github.com/tektoncd/pipeline/pkg/client/clientset/versioned"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/bigkevmcd/tekton-ci/pkg/cel"
	"github.com/bigkevmcd/tekton-ci/pkg/ci"
	"github.com/bigkevmcd/tekton-ci/pkg/git"
	"github.com/bigkevmcd/tekton-ci/pkg/logger"
	"github.com/bigkevmcd/tekton-ci/pkg/metrics"
	"github.com/bigkevmcd/tekton-ci/pkg/volumes"
)

const (
	pipelineFilename = ".tekton_ci.yaml"
)

var defaultVolumeSize = resource.MustParse("1Gi")

// Handler implements the GitEventHandler interface and processes
// .tekton_ci.yaml files in a repository.
type Handler struct {
	scmClient      git.SCM
	log            logger.Logger
	pipelineClient pipelineclientset.Interface
	namespace      string
	volumeCreator  volumes.Creator
	config         *Configuration
	metrics        *metrics.PrometheusMetrics
}

// New creates and returns a new Handler for converting ci.Pipelines into
// PipelineRuns.
func New(scmClient git.SCM, pipelineClient pipelineclientset.Interface, volumeCreator volumes.Creator, m *metrics.PrometheusMetrics, cfg *Configuration, namespace string, l logger.Logger) *Handler {
	return &Handler{
		scmClient:      scmClient,
		pipelineClient: pipelineClient,
		volumeCreator:  volumeCreator,
		log:            l,
		config:         cfg,
		metrics:        m,
		namespace:      namespace,
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hook, err := h.scmClient.ParseWebhookRequest(r)
	if err != nil {
		h.log.Errorf("error parsing webhook: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		h.metrics.CountInvalidHook()
		return
	}

	h.metrics.CountHook(hook)

	if hook.Kind() == scm.WebhookKindPush {
		h.push(r.Context(), hook.(*scm.PushHook), w)
	}
}

// TODO: detect deleted events and don't execute.
func (h *Handler) push(ctx context.Context, evt *scm.PushHook, w http.ResponseWriter) {
	repo := fmt.Sprintf("%s/%s", evt.Repo.Namespace, evt.Repo.Name)
	h.log.Infow("processing push event", "repo", repo, "sha", evt.Commit.Sha)
	content, err := h.scmClient.FileContents(ctx, repo, pipelineFilename, evt.Commit.Sha)
	// This does not return an error if the pipeline definition can't be found.
	if git.IsNotFound(err) {
		h.log.Infof("no pipeline definition found in %s", repo)
		return
	}
	if err != nil {
		h.log.Errorf("error fetching pipeline file: %s", err)
		// TODO: should this return a 404?
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	celCtx, err := cel.New(evt)
	if err != nil {
		h.log.Errorf("error fetching pipeline file: %s", err)
		// TODO: should this return a 404?
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
	parsed, err := ci.Parse(bytes.NewReader(content))
	if err != nil {
		h.log.Errorf("error parsing pipeline definition: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	vc, err := h.volumeCreator.Create(h.namespace, defaultVolumeSize)
	if err != nil {
		h.log.Errorf("error creating volume: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	pr, err := Convert(parsed, h.log, h.config, sourceFromPushEvent(evt), vc.ObjectMeta.Name, celCtx)
	if err != nil {
		h.log.Errorf("error converting pipeline to pipelinerun: %s %#v", err, celCtx.Data)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	created, err := h.pipelineClient.TektonV1beta1().PipelineRuns(h.namespace).Create(pr)
	if err != nil {
		h.log.Errorf("error creating pipelinerun file: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	b, err := json.Marshal(created)
	if err != nil {
		h.log.Errorf("error marshaling response: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(b)
	if err != nil {
		h.log.Errorf("error writing response: %s", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.log.Infow("completed request")
}

func sourceFromPushEvent(p *scm.PushHook) *Source {
	return &Source{
		RepoURL: p.Repo.Clone,
		Ref:     p.Commit.Sha,
	}
}
