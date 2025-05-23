package builder

import (
	"context"

	platformv1alpha1 "github.com/kagenti/operator/platform/api/v1alpha1"
)

type Builder interface {
	Build(ctx context.Context, component *platformv1alpha1.Component) error

	Cancel(ctx context.Context, component *platformv1alpha1.Component) error

	GetStatus(ctx context.Context, component *platformv1alpha1.Component) (platformv1alpha1.BuildStatus, error)

	CheckStatus(ctx context.Context, component *platformv1alpha1.Component) error

	Cleanup(ctx context.Context, component *platformv1alpha1.Component) error
}
