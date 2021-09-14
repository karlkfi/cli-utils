// Copyright 2019 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package apply

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog/v2"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"sigs.k8s.io/cli-utils/pkg/apply/event"
	"sigs.k8s.io/cli-utils/pkg/apply/filter"
	"sigs.k8s.io/cli-utils/pkg/apply/poller"
	"sigs.k8s.io/cli-utils/pkg/apply/prune"
	"sigs.k8s.io/cli-utils/pkg/apply/solver"
	"sigs.k8s.io/cli-utils/pkg/apply/taskrunner"
	"sigs.k8s.io/cli-utils/pkg/common"
	"sigs.k8s.io/cli-utils/pkg/inventory"
	"sigs.k8s.io/cli-utils/pkg/object"
)

// NewDestroyer returns a new destroyer. It will set up the ApplyOptions and
// PruneOptions which are responsible for capturing any command line flags.
// It currently requires IOStreams, but this is a legacy from when
// the ApplyOptions were responsible for printing progress. This is now
// handled by a separate printer with the KubectlPrinterAdapter bridging
// between the two.
func NewDestroyer(factory cmdutil.Factory, invClient inventory.InventoryClient, statusPoller poller.Poller) (*Destroyer, error) {
	pruner, err := prune.NewPruner(factory, invClient)
	if err != nil {
		return nil, fmt.Errorf("error setting up PruneOptions: %w", err)
	}
	return &Destroyer{
		pruner:       pruner,
		statusPoller: statusPoller,
		factory:      factory,
		invClient:    invClient,
	}, nil
}

// Destroyer performs the step of grabbing all the previous inventory objects and
// prune them. This also deletes all the previous inventory objects
type Destroyer struct {
	pruner       *prune.Pruner
	statusPoller poller.Poller
	factory      cmdutil.Factory
	invClient    inventory.InventoryClient
}

type DestroyerOptions struct {
	// InventoryPolicy defines the inventory policy of apply.
	InventoryPolicy inventory.InventoryPolicy

	// DryRunStrategy defines whether changes should actually be performed,
	// or if it is just talk and no action.
	DryRunStrategy common.DryRunStrategy

	// DeleteTimeout defines how long we should wait for resources
	// to be fully deleted.
	DeleteTimeout time.Duration

	// DeletePropagationPolicy defines the deletion propagation policy
	// that should be used. If this is not provided, the default is to
	// use the Background policy.
	DeletePropagationPolicy metav1.DeletionPropagation

	// EmitStatusEvents defines whether status events should be
	// emitted on the eventChannel to the caller.
	EmitStatusEvents bool

	// PollInterval defines how often we should poll for the status
	// of resources.
	PollInterval time.Duration
}

func (do DestroyerOptions) PruneOptions() prune.Options {
	return prune.Options{
		DryRunStrategy:            do.DryRunStrategy,
		DeleteTimeout:             do.DeleteTimeout,
		DeletionPropagationPolicy: do.DeletePropagationPolicy,
		// Always remove pruned resources from inventory when destroying.
		Destroy: true,
	}
}

func (do DestroyerOptions) SolverOptions() solver.Options {
	return solver.Options{
		DeleteTimeout:           do.DeleteTimeout,
		DryRunStrategy:          do.DryRunStrategy,
		DeletePropagationPolicy: do.DeletePropagationPolicy,
	}
}

func (do DestroyerOptions) TaskRunnerOptions() taskrunner.Options {
	return taskrunner.Options{
		UseCache:         true,
		PollInterval:     do.PollInterval,
		EmitStatusEvents: do.EmitStatusEvents,
	}
}

func setDestroyerDefaults(o *DestroyerOptions) {
	if o.PollInterval == time.Duration(0) {
		o.PollInterval = poller.DefaultPollInterval
	}
	if o.DeletePropagationPolicy == "" {
		o.DeletePropagationPolicy = metav1.DeletePropagationBackground
	}
}

// Run performs the destroy step. Passes the inventory object. This
// happens asynchronously on progress and any errors are reported
// back on the event channel.
func (d *Destroyer) Run(ctx context.Context, inv inventory.InventoryInfo, opts DestroyerOptions) <-chan event.Event {
	eventChannel := make(chan event.Event)
	setDestroyerDefaults(&opts)
	go func() {
		defer close(eventChannel)
		// Retrieve the objects to be deleted from the cluster. Second parameter is empty
		// because no local objects returns all inventory objects for deletion.
		emptyLocalObjs := []*unstructured.Unstructured{}
		deleteObjs, err := d.pruner.GetPruneObjs(
			ctx,
			inv,
			emptyLocalObjs,
			opts.PruneOptions(),
		)
		if err != nil {
			handleError(eventChannel, err)
			return
		}
		mapper, err := d.factory.ToRESTMapper()
		if err != nil {
			handleError(eventChannel, err)
			return
		}
		klog.V(4).Infoln("destroyer building task queue...")
		taskBuilder := &solver.TaskQueueBuilder{
			Pruner:    d.pruner,
			Factory:   d.factory,
			Mapper:    mapper,
			InvClient: d.invClient,
			Destroy:   opts.PruneOptions().Destroy,
		}
		solverOpts := opts.SolverOptions()
		deleteFilters := []filter.ValidationFilter{
			filter.PreventRemoveFilter{},
			filter.InventoryPolicyFilter{
				Inv:       inv,
				InvPolicy: opts.InventoryPolicy,
			},
		}

		// Build the ordered set of tasks to execute.
		// Destroyer always prunes
		taskBuilder.AppendPruneWaitTasks(deleteObjs, deleteFilters, solverOpts)
		taskBuilder.AppendDeleteInvTask(inv, opts.DryRunStrategy)
		taskQueue, err := taskBuilder.Build()
		if err != nil {
			handleError(eventChannel, err)
		}

		// Send event to inform the caller about the resources that
		// will be pruned.
		eventChannel <- event.Event{
			Type: event.InitType,
			InitEvent: event.InitEvent{
				ActionGroups: taskQueue.ToActionGroups(),
			},
		}
		// Create a new TaskStatusRunner to execute the taskQueue.
		klog.V(4).Infoln("destroyer building TaskStatusRunner...")
		deleteIds := object.UnstructuredsToObjMetasOrDie(deleteObjs)
		runner := taskrunner.NewTaskStatusRunner(deleteIds, d.statusPoller)
		klog.V(4).Infoln("destroyer running TaskStatusRunner...")
		// TODO(seans): Make the poll interval configurable like the applier.
		err = runner.Run(
			ctx,
			taskQueue.ToChannel(),
			eventChannel,
			opts.TaskRunnerOptions(),
		)
		if err != nil {
			handleError(eventChannel, err)
		}
	}()
	return eventChannel
}
