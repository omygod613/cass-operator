package reconciliation

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"

	api "github.com/riptano/dse-operator/operator/pkg/apis/cassandra/v1alpha2"
	"github.com/riptano/dse-operator/operator/pkg/mocks"
)

func TestCalculateReconciliationActions(t *testing.T) {
	rc, _, cleanupMockScr := setupTest()
	defer cleanupMockScr()

	datacenterReconcile, reconcileRacks, reconcileServices := getReconcilers(rc)

	result, err := calculateReconciliationActions(rc, datacenterReconcile, reconcileRacks, reconcileServices, &ReconcileCassandraDatacenter{client: rc.Client})
	assert.NoErrorf(t, err, "Should not have returned an error while calculating reconciliation actions")
	assert.NotNil(t, result, "Result should not be nil")

	// Add a service and check the logic

	fakeClient, _ := fakeClientWithService(rc.Datacenter)
	rc.Client = *fakeClient

	result, err = calculateReconciliationActions(rc, datacenterReconcile, reconcileRacks, reconcileServices, &ReconcileCassandraDatacenter{client: rc.Client})
	assert.NoErrorf(t, err, "Should not have returned an error while calculating reconciliation actions")
	assert.NotNil(t, result, "Result should not be nil")
}

func TestCalculateReconciliationActions_GetServiceError(t *testing.T) {
	rc, _, cleanupMockScr := setupTest()
	defer cleanupMockScr()

	mockClient := &mocks.Client{}
	rc.Client = mockClient

	k8sMockClientGet(mockClient, fmt.Errorf(""))
	k8sMockClientUpdate(mockClient, nil).Times(1)
	// k8sMockClientCreate(mockClient, nil)

	datacenterReconcile, reconcileRacks, reconcileServices := getReconcilers(rc)

	result, err := calculateReconciliationActions(rc, datacenterReconcile, reconcileRacks, reconcileServices, &ReconcileCassandraDatacenter{client: rc.Client})
	assert.Errorf(t, err, "Should have returned an error while calculating reconciliation actions")
	assert.Equal(t, reconcile.Result{Requeue: true}, result, "Should requeue request")

	mockClient.AssertExpectations(t)
}

func TestCalculateReconciliationActions_FailedUpdate(t *testing.T) {
	rc, _, cleanupMockScr := setupTest()
	defer cleanupMockScr()

	mockClient := &mocks.Client{}
	rc.Client = mockClient

	k8sMockClientUpdate(mockClient, fmt.Errorf("failed to update CassandraDatacenter with removed finalizers"))

	datacenterReconcile, reconcileRacks, reconcileServices := getReconcilers(rc)
	result, err := calculateReconciliationActions(rc, datacenterReconcile, reconcileRacks, reconcileServices, &ReconcileCassandraDatacenter{client: rc.Client})
	assert.Errorf(t, err, "Should have returned an error while calculating reconciliation actions")
	assert.Equal(t, reconcile.Result{Requeue: true}, result, "Should requeue request")

	mockClient.AssertExpectations(t)
}

func TestProcessDeletion_FailedDelete(t *testing.T) {
	rc, _, cleanupMockScr := setupTest()
	defer cleanupMockScr()

	mockClient := &mocks.Client{}
	rc.Client = mockClient

	k8sMockClientList(mockClient, nil).
		Run(func(args mock.Arguments) {
			arg := args.Get(1).(*v1.PersistentVolumeClaimList)
			arg.Items = []v1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "pvc-1",
				},
			}}
		})

	k8sMockClientDelete(mockClient, fmt.Errorf(""))
	k8sMockClientUpdate(mockClient, nil).Times(1)

	now := metav1.Now()
	rc.Datacenter.SetDeletionTimestamp(&now)

	datacenterReconcile, reconcileRacks, reconcileServices := getReconcilers(rc)
	result, err := calculateReconciliationActions(rc, datacenterReconcile, reconcileRacks, reconcileServices, &ReconcileCassandraDatacenter{client: rc.Client})
	assert.Errorf(t, err, "Should have returned an error while calculating reconciliation actions")
	assert.Equal(t, reconcile.Result{Requeue: true}, result, "Should requeue request")

	mockClient.AssertExpectations(t)
}

func TestReconcile(t *testing.T) {
	// Set up verbose logging
	logger := logf.ZapLogger(true)
	logf.SetLogger(logger)

	var (
		name            = "cluster-example-cluster.dc-example-datacenter"
		namespace       = "default"
		size      int32 = 2
	)
	storageSize := resource.MustParse("1Gi")
	storageName := "server-data"
	storageConfig := api.StorageConfig{
		CassandraDataVolumeClaimSpec: &corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageName,
			AccessModes:      []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{"storage": storageSize},
			},
		},
	}

	// Instance a CassandraDatacenter
	dc := &api.CassandraDatacenter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: api.CassandraDatacenterSpec{
			ManagementApiAuth: api.ManagementApiAuthConfig{
				Insecure: &api.ManagementApiAuthInsecureConfig{},
			},
			Size:          size,
			StorageConfig: storageConfig,
		},
	}

	// Objects to keep track of
	trackObjects := []runtime.Object{
		dc,
	}

	s := scheme.Scheme
	s.AddKnownTypes(api.SchemeGroupVersion, dc)

	fakeClient := fake.NewFakeClient(trackObjects...)

	r := &ReconcileCassandraDatacenter{
		client:   fakeClient,
		scheme:   s,
		recorder: record.NewFakeRecorder(100),
	}

	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	result, err := r.Reconcile(request)
	if err != nil {
		t.Fatalf("Reconciliation Failure: (%v)", err)
	}

	if result != (reconcile.Result{Requeue: true}) {
		t.Error("Reconcile did not return a correct result.")
	}
}

func TestReconcile_NotFound(t *testing.T) {
	// Set up verbose logging
	logger := logf.ZapLogger(true)
	logf.SetLogger(logger)

	var (
		name            = "datacenter-example"
		namespace       = "default"
		size      int32 = 2
	)

	storageSize := resource.MustParse("1Gi")
	storageName := "server-data"
	storageConfig := api.StorageConfig{
		CassandraDataVolumeClaimSpec: &corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageName,
			AccessModes:      []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{"storage": storageSize},
			},
		},
	}

	// Instance a CassandraDatacenter
	dc := &api.CassandraDatacenter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: api.CassandraDatacenterSpec{
			ManagementApiAuth: api.ManagementApiAuthConfig{
				Insecure: &api.ManagementApiAuthInsecureConfig{},
			},
			Size:          size,
			StorageConfig: storageConfig,
		},
	}

	// Objects to keep track of
	trackObjects := []runtime.Object{}

	s := scheme.Scheme
	s.AddKnownTypes(api.SchemeGroupVersion, dc)

	fakeClient := fake.NewFakeClient(trackObjects...)

	r := &ReconcileCassandraDatacenter{
		client: fakeClient,
		scheme: s,
	}

	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	result, err := r.Reconcile(request)
	if err != nil {
		t.Fatalf("Reconciliation Failure: (%v)", err)
	}

	expected := reconcile.Result{}
	if result != expected {
		t.Error("expected to get a zero-value reconcile.Result")
	}
}

func TestReconcile_Error(t *testing.T) {
	// Set up verbose logging
	logger := logf.ZapLogger(true)
	logf.SetLogger(logger)

	var (
		name            = "datacenter-example"
		namespace       = "default"
		size      int32 = 2
	)

	storageSize := resource.MustParse("1Gi")
	storageName := "server-data"
	storageConfig := api.StorageConfig{
		CassandraDataVolumeClaimSpec: &corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageName,
			AccessModes:      []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{"storage": storageSize},
			},
		},
	}

	// Instance a CassandraDatacenter
	dc := &api.CassandraDatacenter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: api.CassandraDatacenterSpec{
			ManagementApiAuth: api.ManagementApiAuthConfig{
				Insecure: &api.ManagementApiAuthInsecureConfig{},
			},
			Size:          size,
			StorageConfig: storageConfig,
		},
	}

	// Objects to keep track of

	s := scheme.Scheme
	s.AddKnownTypes(api.SchemeGroupVersion, dc)

	mockClient := &mocks.Client{}
	k8sMockClientGet(mockClient, fmt.Errorf(""))

	r := &ReconcileCassandraDatacenter{
		client: mockClient,
		scheme: s,
	}

	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	result, err := r.Reconcile(request)
	if err == nil {
		t.Fatalf("Reconciliation should have failed")
	}

	if result != (reconcile.Result{Requeue: true}) {
		t.Error("Reconcile did not return an empty result.")
	}
}

func TestReconcile_CassandraDatacenterToBeDeleted(t *testing.T) {
	// Set up verbose logging
	logger := logf.ZapLogger(true)
	logf.SetLogger(logger)

	var (
		name            = "datacenter-example"
		namespace       = "default"
		size      int32 = 2
	)

	storageSize := resource.MustParse("1Gi")
	storageName := "server-data"
	storageConfig := api.StorageConfig{
		CassandraDataVolumeClaimSpec: &corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageName,
			AccessModes:      []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{"storage": storageSize},
			},
		},
	}

	// Instance a CassandraDatacenter
	now := metav1.Now()
	dc := &api.CassandraDatacenter{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         namespace,
			DeletionTimestamp: &now,
			Finalizers:        nil,
		},
		Spec: api.CassandraDatacenterSpec{
			ManagementApiAuth: api.ManagementApiAuthConfig{
				Insecure: &api.ManagementApiAuthInsecureConfig{},
			},
			Size:          size,
			StorageConfig: storageConfig,
		},
	}

	// Objects to keep track of
	trackObjects := []runtime.Object{
		dc,
	}

	s := scheme.Scheme
	s.AddKnownTypes(api.SchemeGroupVersion, dc)

	fakeClient := fake.NewFakeClient(trackObjects...)

	r := &ReconcileCassandraDatacenter{
		client: fakeClient,
		scheme: s,
	}

	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      name,
			Namespace: namespace,
		},
	}

	result, err := r.Reconcile(request)
	if err != nil {
		t.Fatalf("Reconciliation Failure: (%v)", err)
	}

	if result != (reconcile.Result{}) {
		t.Error("Reconcile did not return an empty result.")
	}
}
