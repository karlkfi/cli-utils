// Copyright 2020 The Kubernetes Authors.
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/cli-utils/pkg/kstatus/polling/event"
	"sigs.k8s.io/cli-utils/pkg/object"
)

func TestCollectorStopsWhenEventChannelIsClosed(t *testing.T) {
	var identifiers object.ObjMetadataSet

	collector := NewResourceStatusCollector(identifiers)

	eventCh := make(chan event.Event)
	stopCh := make(chan struct{})
	defer close(stopCh)

	completedCh := collector.Listen(eventCh)

	timer := time.NewTimer(3 * time.Second)

	close(eventCh)
	select {
	case <-timer.C:
		t.Errorf("expected collector to close the completedCh, but it didn't")
	case <-completedCh:
		timer.Stop()
	}
}

func TestCollectorWithFatalError(t *testing.T) {
	var identifiers object.ObjMetadataSet

	collector := NewResourceStatusCollector(identifiers)

	eventCh := make(chan event.Event)

	completedCh := collector.Listen(eventCh)

	exampleErr := fmt.Errorf("this is a test error")
	eventCh <- event.Event{
		Type:  event.ErrorEvent,
		Error: exampleErr,
	}

	var err error
	timer := time.NewTimer(3 * time.Second)
	close(eventCh)
	select {
	case <-timer.C:
		t.Errorf("expected collector to close the completedCh, but it didn't")
	case msg, ok := <-completedCh:
		if ok {
			err = msg.Err
		} else {
			timer.Stop()
		}
	}

	if !errors.Is(err, exampleErr) {
		t.Errorf("expected exampleErr, but found %v", err)
	}
}

var (
	deploymentGVK       = appsv1.SchemeGroupVersion.WithKind("Deployment")
	statefulSetGVK      = appsv1.SchemeGroupVersion.WithKind("StatefulSet")
	resourceIdentifiers = map[string]object.ObjMetadata{
		"deployment": {
			GroupKind: deploymentGVK.GroupKind(),
			Name:      "Foo",
			Namespace: "default",
		},
		"statefulSet": {
			GroupKind: statefulSetGVK.GroupKind(),
			Name:      "Bar",
			Namespace: "default",
		},
	}
)

func TestCollectorEventProcessing(t *testing.T) {
	testCases := map[string]struct {
		identifiers object.ObjMetadataSet
		events      []event.Event
	}{
		"no resources and no events": {},
		"single resource and single event": {
			identifiers: object.ObjMetadataSet{
				resourceIdentifiers["deployment"],
			},
			events: []event.Event{
				{
					Type: event.ResourceUpdateEvent,
					Resource: &event.ResourceStatus{
						Identifier: resourceIdentifiers["deployment"],
					},
				},
			},
		},
		"multiple resources and multiple events": {
			identifiers: object.ObjMetadataSet{
				resourceIdentifiers["deployment"],
				resourceIdentifiers["statefulSet"],
			},
			events: []event.Event{
				{
					Type: event.ResourceUpdateEvent,
					Resource: &event.ResourceStatus{
						Identifier: resourceIdentifiers["deployment"],
					},
				},
				{
					Type: event.ResourceUpdateEvent,
					Resource: &event.ResourceStatus{
						Identifier: resourceIdentifiers["statefulSet"],
					},
				},
				{
					Type: event.ResourceUpdateEvent,
					Resource: &event.ResourceStatus{
						Identifier: resourceIdentifiers["deployment"],
					},
				},
				{
					Type: event.ResourceUpdateEvent,
					Resource: &event.ResourceStatus{
						Identifier: resourceIdentifiers["statefulSet"],
					},
				},
			},
		},
	}

	for tn, tc := range testCases {
		t.Run(tn, func(t *testing.T) {
			collector := NewResourceStatusCollector(tc.identifiers)

			eventCh := make(chan event.Event)
			defer close(eventCh)

			collector.Listen(eventCh)

			var latestEvent *event.Event
			latestEventByIdentifier := make(map[object.ObjMetadata]event.Event)
			for _, e := range tc.events {
				if e.Resource != nil {
					latestEventByIdentifier[e.Resource.Identifier] = e
				}
				latestEvent = &e
				eventCh <- e
			}
			// Give the collector some time to process the event.
			<-time.NewTimer(time.Second).C

			observation := collector.LatestObservation()

			var expectedObservation *Observation
			if latestEvent != nil {
				expectedObservation = &Observation{
					LastEventType: latestEvent.Type,
				}
			} else {
				expectedObservation = &Observation{}
			}

			var resourceStatuses event.ResourceStatuses
			for _, e := range latestEventByIdentifier {
				resourceStatuses = append(resourceStatuses, e.Resource)
			}
			sort.Sort(resourceStatuses)
			expectedObservation.ResourceStatuses = resourceStatuses

			assert.Equal(t, expectedObservation, observation)
		})
	}
}
