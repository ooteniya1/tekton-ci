package dsl

import (
	"k8s.io/apimachinery/pkg/api/resource"
)

// Configuration provides options for the conversion to PipelineRuns.
type Configuration struct {
	ArchiverImage             string            // Executed for tasks that have artifacts to archive.
	ArchiveURL                string            // Passed to the archiver along with the artifact paths.
	PipelineRunPrefix         string            // Used in the generateName property of the created PipelineRun.
	DefaultServiceAccountName string            // The default service account for created PipelineRuns.
	VolumeSize                resource.Quantity // The size to create volumes as.
}
