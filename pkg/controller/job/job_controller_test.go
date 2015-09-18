/*
Copyright 2015 The Kubernetes Authors All rights reserved.

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

package job

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/testapi"
	"k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/apis/experimental"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/client/unversioned/testclient"
	"k8s.io/kubernetes/pkg/controller"
	"k8s.io/kubernetes/pkg/runtime"
	"k8s.io/kubernetes/pkg/util"
	"k8s.io/kubernetes/pkg/watch"
)

// Give each test that starts a background controller up to 1/2 a second.
// Since we need to start up a goroutine to test watch, this routine needs
// to get cpu before the test can complete. If the test is starved of cpu,
// the watch test will take up to 1/2 a second before timing out.
const controllerTimeout = 500 * time.Millisecond

var alwaysReady = func() bool { return true }

type FakePodControl struct {
	podSpec       []api.PodTemplateSpec
	deletePodName []string
	lock          sync.Mutex
	err           error
}

func (f *FakePodControl) CreatePods(namespace string, spec *api.PodTemplateSpec, object runtime.Object) error {
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.err != nil {
		return f.err
	}
	f.podSpec = append(f.podSpec, *spec)
	return nil
}

func (f *FakePodControl) CreatePodsOnNode(nodeName, namespace string, template *api.PodTemplateSpec, object runtime.Object) error {
	return nil
}

func (f *FakePodControl) DeletePod(namespace string, podName string) error {
	f.lock.Lock()
	defer f.lock.Unlock()
	if f.err != nil {
		return f.err
	}
	f.deletePodName = append(f.deletePodName, podName)
	return nil
}
func (f *FakePodControl) clear() {
	f.lock.Lock()
	defer f.lock.Unlock()
	f.deletePodName = []string{}
	f.podSpec = []api.PodTemplateSpec{}
}

func newJob(parallelism, completions int, restartPolicy api.RestartPolicy) *experimental.Job {
	return &experimental.Job{
		ObjectMeta: api.ObjectMeta{
			Name:      "foobar",
			Namespace: api.NamespaceDefault,
		},
		Spec: experimental.JobSpec{
			Parallelism: &parallelism,
			Completions: &completions,
			Selector:    map[string]string{"foo": "bar"},
			Template: &api.PodTemplateSpec{
				ObjectMeta: api.ObjectMeta{
					Labels: map[string]string{
						"foo": "bar",
					},
				},
				Spec: api.PodSpec{
					RestartPolicy: restartPolicy,
					Containers: []api.Container{
						{Image: "foo/bar"},
					},
				},
			},
		},
	}
}

func getKey(job *experimental.Job, t *testing.T) string {
	if key, err := controller.KeyFunc(job); err != nil {
		t.Errorf("Unexpected error getting key for job %v: %v", job.Name, err)
		return ""
	} else {
		return key
	}
}

// create count pods with the given phase for the given job
func newPodList(count int, status api.PodPhase, job *experimental.Job) []api.Pod {
	pods := []api.Pod{}
	for i := 0; i < count; i++ {
		newPod := api.Pod{
			ObjectMeta: api.ObjectMeta{
				Name:      fmt.Sprintf("pod-%v", unversioned.Now().UnixNano()),
				Labels:    job.Spec.Selector,
				Namespace: job.Namespace,
			},
			Status: api.PodStatus{Phase: status},
		}
		pods = append(pods, newPod)
	}
	return pods
}

func TestControllerSyncJob(t *testing.T) {
	testCases := map[string]struct {
		// job setup
		parallelism   int
		completions   int
		restartPolicy api.RestartPolicy

		// pod setup
		podControllerError error
		activePods         int
		successfulPods     int
		unsuccessfulPods   int

		// expectations
		expectedCreations    int
		expectedDeletions    int
		expectedActive       int
		expectedSuccessful   int
		expectedUnsuccessful int
		expectedComplete     bool
	}{
		"job start": {
			2, 5, api.RestartPolicyOnFailure,
			nil, 0, 0, 0,
			2, 0, 2, 0, 0, false,
		},
		"correct # of pods": {
			2, 5, api.RestartPolicyOnFailure,
			nil, 2, 0, 0,
			0, 0, 2, 0, 0, false,
		},
		"too few active pods": {
			2, 5, api.RestartPolicyOnFailure,
			nil, 1, 1, 0,
			1, 0, 2, 1, 0, false,
		},
		"too few active pods, with controller error": {
			2, 5, api.RestartPolicyOnFailure,
			fmt.Errorf("Fake error"), 1, 1, 0,
			0, 0, 1, 1, 0, false,
		},
		"too many active pods": {
			2, 5, api.RestartPolicyOnFailure,
			nil, 3, 0, 0,
			0, 1, 2, 0, 0, false,
		},
		"too many active pods, with controller error": {
			2, 5, api.RestartPolicyOnFailure,
			fmt.Errorf("Fake error"), 3, 0, 0,
			0, 0, 3, 0, 0, false,
		},
		"failed pod and OnFailure restart policy": {
			2, 5, api.RestartPolicyOnFailure,
			nil, 1, 1, 1,
			1, 0, 2, 1, 0, false,
		},
		"failed pod and Never restart policy": {
			2, 5, api.RestartPolicyNever,
			nil, 1, 1, 1,
			1, 0, 2, 1, 1, false,
		},
		"job finish and OnFailure restart policy": {
			2, 5, api.RestartPolicyOnFailure,
			nil, 0, 5, 0,
			0, 0, 0, 5, 0, true,
		},
		"job finish and Never restart policy": {
			2, 5, api.RestartPolicyNever,
			nil, 0, 2, 3,
			0, 0, 0, 2, 3, true,
		},
		"more active pods than completions": {
			2, 5, api.RestartPolicyOnFailure,
			nil, 10, 0, 0,
			0, 8, 2, 0, 0, false,
		},
		"status change": {
			2, 5, api.RestartPolicyOnFailure,
			nil, 2, 2, 0,
			0, 0, 2, 2, 0, false,
		},
	}

	for name, tc := range testCases {
		// job manager setup
		client := client.NewOrDie(&client.Config{Host: "", Version: testapi.Experimental.Version()})
		manager := NewJobManager(client)
		fakePodControl := FakePodControl{err: tc.podControllerError}
		manager.podControl = &fakePodControl
		manager.podStoreSynced = alwaysReady
		var actual *experimental.Job
		manager.updateHandler = func(job *experimental.Job) error {
			actual = job
			return nil
		}

		// job & pods setup
		job := newJob(tc.parallelism, tc.completions, tc.restartPolicy)
		manager.jobStore.Store.Add(job)
		for _, pod := range newPodList(tc.activePods, api.PodRunning, job) {
			manager.podStore.Store.Add(&pod)
		}
		for _, pod := range newPodList(tc.successfulPods, api.PodSucceeded, job) {
			manager.podStore.Store.Add(&pod)
		}
		for _, pod := range newPodList(tc.unsuccessfulPods, api.PodFailed, job) {
			manager.podStore.Store.Add(&pod)
		}

		// run
		err := manager.syncJob(getKey(job, t))
		if err != nil {
			t.Errorf("%s: unexpected error when syncing jobs %v", err)
		}

		// validate created/deleted pods
		if len(fakePodControl.podSpec) != tc.expectedCreations {
			t.Errorf("%s: unexpected number of creates.  Expected %d, saw %d\n", name, tc.expectedCreations, len(fakePodControl.podSpec))
		}
		if len(fakePodControl.deletePodName) != tc.expectedDeletions {
			t.Errorf("%s: unexpected number of deletes.  Expected %d, saw %d\n", name, tc.expectedDeletions, len(fakePodControl.deletePodName))
		}
		// validate status
		if actual.Status.Active != tc.expectedActive {
			t.Errorf("%s: unexpected number of active pods.  Expected %d, saw %d\n", name, tc.expectedActive, actual.Status.Active)
		}
		if actual.Status.Successful != tc.expectedSuccessful {
			t.Errorf("%s: unexpected number of successful pods.  Expected %d, saw %d\n", name, tc.expectedSuccessful, actual.Status.Successful)
		}
		if actual.Status.Unsuccessful != tc.expectedUnsuccessful {
			t.Errorf("%s: unexpected number of unsuccessful pods.  Expected %d, saw %d\n", name, tc.expectedUnsuccessful, actual.Status.Unsuccessful)
		}
		// validate conditions
		if tc.expectedComplete {
			completed := false
			for _, v := range actual.Status.Conditions {
				if v.Type == experimental.JobComplete && v.Status == api.ConditionTrue {
					completed = true
					break
				}
			}
			if !completed {
				t.Errorf("%s: expected completion condition.  Got %v", name, actual.Status.Conditions)
			}
		}
	}
}

func TestSyncJobDeleted(t *testing.T) {
	client := client.NewOrDie(&client.Config{Host: "", Version: testapi.Experimental.Version()})
	manager := NewJobManager(client)
	fakePodControl := FakePodControl{}
	manager.podControl = &fakePodControl
	manager.podStoreSynced = alwaysReady
	manager.updateHandler = func(job *experimental.Job) error { return nil }
	job := newJob(2, 2, api.RestartPolicyOnFailure)
	err := manager.syncJob(getKey(job, t))
	if err != nil {
		t.Errorf("Unexpected error when syncing jobs %v", err)
	}
	if len(fakePodControl.podSpec) != 0 {
		t.Errorf("Unexpected number of creates.  Expected %d, saw %d\n", 0, len(fakePodControl.podSpec))
	}
	if len(fakePodControl.deletePodName) != 0 {
		t.Errorf("Unexpected number of deletes.  Expected %d, saw %d\n", 0, len(fakePodControl.deletePodName))
	}
}

func TestSyncJobUpdateRequeue(t *testing.T) {
	client := client.NewOrDie(&client.Config{Host: "", Version: testapi.Experimental.Version()})
	manager := NewJobManager(client)
	fakePodControl := FakePodControl{}
	manager.podControl = &fakePodControl
	manager.podStoreSynced = alwaysReady
	manager.updateHandler = func(job *experimental.Job) error { return fmt.Errorf("Fake error") }
	job := newJob(2, 2, api.RestartPolicyOnFailure)
	manager.jobStore.Store.Add(job)
	err := manager.syncJob(getKey(job, t))
	if err != nil {
		t.Errorf("Unxpected error when syncing jobs, got %v", err)
	}
	ch := make(chan interface{})
	go func() {
		item, _ := manager.queue.Get()
		ch <- item
	}()
	select {
	case key := <-ch:
		expectedKey := getKey(job, t)
		if key != expectedKey {
			t.Errorf("Expected requeue of job with key %s got %s", expectedKey, key)
		}
	case <-time.After(controllerTimeout):
		manager.queue.ShutDown()
		t.Errorf("Expected to find a job in the queue, found none.")
	}
}

func TestJobPodLookup(t *testing.T) {
	client := client.NewOrDie(&client.Config{Host: "", Version: testapi.Experimental.Version()})
	manager := NewJobManager(client)
	manager.podStoreSynced = alwaysReady
	testCases := []struct {
		job *experimental.Job
		pod *api.Pod

		expectedName string
	}{
		// pods without labels don't match any job
		{
			job: &experimental.Job{
				ObjectMeta: api.ObjectMeta{Name: "basic"},
			},
			pod: &api.Pod{
				ObjectMeta: api.ObjectMeta{Name: "foo1", Namespace: api.NamespaceAll},
			},
			expectedName: "",
		},
		// matching labels, different namespace
		{
			job: &experimental.Job{
				ObjectMeta: api.ObjectMeta{Name: "foo"},
				Spec: experimental.JobSpec{
					Selector: map[string]string{"foo": "bar"},
				},
			},
			pod: &api.Pod{
				ObjectMeta: api.ObjectMeta{
					Name:      "foo2",
					Namespace: "ns",
					Labels:    map[string]string{"foo": "bar"},
				},
			},
			expectedName: "",
		},
		// matching ns and labels returns
		{
			job: &experimental.Job{
				ObjectMeta: api.ObjectMeta{Name: "bar", Namespace: "ns"},
				Spec: experimental.JobSpec{
					Selector: map[string]string{"foo": "bar"},
				},
			},
			pod: &api.Pod{
				ObjectMeta: api.ObjectMeta{
					Name:      "foo3",
					Namespace: "ns",
					Labels:    map[string]string{"foo": "bar"},
				},
			},
			expectedName: "bar",
		},
	}
	for _, tc := range testCases {
		manager.jobStore.Add(tc.job)
		if job := manager.getPodJob(tc.pod); job != nil {
			if tc.expectedName != job.Name {
				t.Errorf("Got job %+v expected %+v", job.Name, tc.expectedName)
			}
		} else if tc.expectedName != "" {
			t.Errorf("Expected a job %v pod %v, found none", tc.expectedName, tc.pod.Name)
		}
	}
}

type FakeJobExpectations struct {
	*controller.ControllerExpectations
	satisfied    bool
	expSatisfied func()
}

func (fe FakeJobExpectations) SatisfiedExpectations(controllerKey string) bool {
	fe.expSatisfied()
	return fe.satisfied
}

// TestSyncJobExpectations tests that a pod cannot sneak in between counting active pods
// and checking expectations.
func TestSyncJobExpectations(t *testing.T) {
	client := client.NewOrDie(&client.Config{Host: "", Version: testapi.Experimental.Version()})
	manager := NewJobManager(client)
	fakePodControl := FakePodControl{}
	manager.podControl = &fakePodControl
	manager.podStoreSynced = alwaysReady
	manager.updateHandler = func(job *experimental.Job) error { return nil }

	job := newJob(2, 2, api.RestartPolicyOnFailure)
	manager.jobStore.Store.Add(job)
	pods := newPodList(2, api.PodPending, job)
	manager.podStore.Store.Add(&pods[0])

	manager.expectations = FakeJobExpectations{
		controller.NewControllerExpectations(), true, func() {
			// If we check active pods before checking expectataions, the job
			// will create a new replica because it doesn't see this pod, but
			// has fulfilled its expectations.
			manager.podStore.Store.Add(&pods[1])
		},
	}
	manager.syncJob(getKey(job, t))
	if len(fakePodControl.podSpec) != 0 {
		t.Errorf("Unexpected number of creates.  Expected %d, saw %d\n", 0, len(fakePodControl.podSpec))
	}
	if len(fakePodControl.deletePodName) != 0 {
		t.Errorf("Unexpected number of deletes.  Expected %d, saw %d\n", 0, len(fakePodControl.deletePodName))
	}
}

type FakeWatcher struct {
	w *watch.FakeWatcher
	*testclient.Fake
}

func TestWatchJobs(t *testing.T) {
	fakeWatch := watch.NewFake()
	client := &testclient.Fake{}
	client.AddWatchReactor("*", testclient.DefaultWatchReactor(fakeWatch, nil))
	manager := NewJobManager(client)
	manager.podStoreSynced = alwaysReady

	var testJob experimental.Job
	received := make(chan string)

	// The update sent through the fakeWatcher should make its way into the workqueue,
	// and eventually into the syncHandler.
	manager.syncHandler = func(key string) error {

		obj, exists, err := manager.jobStore.Store.GetByKey(key)
		if !exists || err != nil {
			t.Errorf("Expected to find job under key %v", key)
		}
		job := *obj.(*experimental.Job)
		if !api.Semantic.DeepDerivative(job, testJob) {
			t.Errorf("Expected %#v, but got %#v", testJob, job)
		}
		received <- key
		return nil
	}
	// Start only the job watcher and the workqueue, send a watch event,
	// and make sure it hits the sync method.
	stopCh := make(chan struct{})
	defer close(stopCh)
	go manager.jobController.Run(stopCh)
	go util.Until(manager.worker, 10*time.Millisecond, stopCh)

	// We're sending new job to see if it reaches syncHandler.
	testJob.Name = "foo"
	fakeWatch.Add(&testJob)
	select {
	case <-received:
	case <-time.After(controllerTimeout):
		t.Errorf("Expected 1 call but got 0")
	}

	// We're sending fake finished job, to see if it reaches syncHandler - it should not,
	// since we're filtering out finished jobs.
	testJobv2 := experimental.Job{
		ObjectMeta: api.ObjectMeta{Name: "foo"},
		Status: experimental.JobStatus{
			Conditions: []experimental.JobCondition{{
				Type:               experimental.JobComplete,
				Status:             api.ConditionTrue,
				LastProbeTime:      unversioned.Now(),
				LastTransitionTime: unversioned.Now(),
			}},
		},
	}
	fakeWatch.Modify(&testJobv2)

	select {
	case <-received:
		t.Errorf("Expected 0 call but got 1")
	case <-time.After(controllerTimeout):
	}
}

func TestWatchPods(t *testing.T) {
	fakeWatch := watch.NewFake()
	client := &testclient.Fake{}
	client.AddWatchReactor("*", testclient.DefaultWatchReactor(fakeWatch, nil))
	manager := NewJobManager(client)
	manager.podStoreSynced = alwaysReady

	// Put one job and one pod into the store
	testJob := newJob(2, 2, api.RestartPolicyOnFailure)
	manager.jobStore.Store.Add(testJob)
	received := make(chan string)
	// The pod update sent through the fakeWatcher should figure out the managing job and
	// send it into the syncHandler.
	manager.syncHandler = func(key string) error {

		obj, exists, err := manager.jobStore.Store.GetByKey(key)
		if !exists || err != nil {
			t.Errorf("Expected to find job under key %v", key)
		}
		job := obj.(*experimental.Job)
		if !api.Semantic.DeepDerivative(job, testJob) {
			t.Errorf("\nExpected %#v,\nbut got %#v", testJob, job)
		}
		close(received)
		return nil
	}
	// Start only the pod watcher and the workqueue, send a watch event,
	// and make sure it hits the sync method for the right job.
	stopCh := make(chan struct{})
	defer close(stopCh)
	go manager.podController.Run(stopCh)
	go util.Until(manager.worker, 10*time.Millisecond, stopCh)

	pods := newPodList(1, api.PodRunning, testJob)
	testPod := pods[0]
	testPod.Status.Phase = api.PodFailed
	fakeWatch.Add(&testPod)

	select {
	case <-received:
	case <-time.After(controllerTimeout):
		t.Errorf("Expected 1 call but got 0")
	}
}
