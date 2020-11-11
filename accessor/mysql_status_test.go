package accessor

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var intermediateSecret = "intermediate-primary-secret"

var _ = Describe("Get MySQLCluster status", func() {
	It("should initialize MySQL for testing", func() {
		err := initializeMySQL()
		Expect(err).ShouldNot(HaveOccurred())
	})

	It("should get MySQL status", func() {
		_, inf, cluster := getAccessorInfraCluster()

		logger := ctrl.Log.WithName("controllers").WithName("MySQLCluster")
		sts, err := GetMySQLClusterStatus(context.Background(), logger, inf, &cluster)

		Expect(err).ShouldNot(HaveOccurred())
		Expect(sts.InstanceStatus).Should(HaveLen(1))
		Expect(sts.InstanceStatus[0].PrimaryStatus).ShouldNot(BeNil())
		Expect(sts.InstanceStatus[0].ReplicaStatus).ShouldNot(BeNil())
		Expect(sts.InstanceStatus[0].AllRelayLogExecuted).Should(BeTrue())
		Expect(sts.InstanceStatus[0].GlobalVariablesStatus).ShouldNot(BeNil())
		Expect(sts.InstanceStatus[0].CloneStateStatus).ShouldNot(BeNil())
		Expect(*sts.Latest).Should(Equal(0))
	})

	It("should get and validate intermediate primary options", func() {
		_, inf, cluster := getAccessorInfraCluster()
		cluster.Spec.ReplicationSourceSecretName = &intermediateSecret
		err := k8sClient.Create(context.Background(), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
		Expect(err).ShouldNot(HaveOccurred())

		By("setting valid options to api server")
		data := map[string][]byte{
			"PRIMARY_HOST":     []byte("dummy-primary"),
			"PRIMARY_PORT":     []byte("3306"),
			"PRIMARY_USER":     []byte("dummy-user"),
			"PRIMARY_PASSWORD": []byte("dummy-password"),
		}
		var ipSecret corev1.Secret
		ipSecret.ObjectMeta.Name = intermediateSecret
		ipSecret.ObjectMeta.Namespace = namespace
		ipSecret.Data = data
		err = k8sClient.Create(context.Background(), &ipSecret)
		Expect(err).ShouldNot(HaveOccurred())

		By("getting and validating intermediate primary options")
		logger := ctrl.Log.WithName("controllers").WithName("MySQLCluster")
		sts, err := GetMySQLClusterStatus(context.Background(), logger, inf, &cluster)
		Expect(err).ShouldNot(HaveOccurred())
		expect := &IntermediatePrimaryOptions{
			PrimaryHost:     "dummy-primary",
			PrimaryPassword: "dummy-password",
			PrimaryPort:     3306,
			PrimaryUser:     "dummy-user",
		}
		Expect(sts.IntermediatePrimaryOptions).Should(Equal(expect))

		By("setting options without PRIMARY_HOST to api server")
		data = map[string][]byte{
			"PRIMARY_PORT": []byte("3306"),
		}
		ipSecret.ObjectMeta.Name = intermediateSecret
		ipSecret.ObjectMeta.Namespace = namespace
		ipSecret.Data = data
		err = k8sClient.Update(context.Background(), &ipSecret)
		Expect(err).ShouldNot(HaveOccurred())

		By("getting and validating intermediate primary options")
		logger = ctrl.Log.WithName("controllers").WithName("MySQLCluster")
		_, err = GetMySQLClusterStatus(context.Background(), logger, inf, &cluster)
		Expect(err).Should(HaveOccurred())

		By("setting options without INVALID_OPTION to api server")
		data = map[string][]byte{
			"PRIMARY_HOST":   []byte("dummy-primary"),
			"PRIMARY_PORT":   []byte("3306"),
			"INVALID_OPTION": []byte("invalid"),
		}
		ipSecret.ObjectMeta.Name = intermediateSecret
		ipSecret.ObjectMeta.Namespace = namespace
		ipSecret.Data = data
		err = k8sClient.Update(context.Background(), &ipSecret)
		Expect(err).ShouldNot(HaveOccurred())

		By("getting and validating intermediate primary options")
		logger = ctrl.Log.WithName("controllers").WithName("MySQLCluster")
		_, err = GetMySQLClusterStatus(context.Background(), logger, inf, &cluster)
		Expect(err).Should(HaveOccurred())
	})

	It("should get latest instance by comparing GTIDs", func() {
		ctx := context.Background()
		_, inf, _ := getAccessorInfraCluster()
		db, err := inf.GetDB(0)
		Expect(err).ShouldNot(HaveOccurred())

		By("comarping empty instances")
		status := []MySQLInstanceStatus{
			{
				PrimaryStatus: &MySQLPrimaryStatus{
					ExecutedGtidSet: "",
				},
			},
			{
				PrimaryStatus: &MySQLPrimaryStatus{
					ExecutedGtidSet: "",
				},
			},
		}
		idx, err := GetLatestInstance(ctx, db, status)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*idx).Should(Equal(0))

		By("including instance which has empty PrimaryStatus")
		status = []MySQLInstanceStatus{
			{
				PrimaryStatus: nil,
			},
		}
		_, err = GetLatestInstance(ctx, db, status)
		Expect(err.Error()).Should(Equal("cannot compare gtids"))

		By("comparing the same GTIDs")
		status = []MySQLInstanceStatus{
			{
				PrimaryStatus: &MySQLPrimaryStatus{
					ExecutedGtidSet: "3E11FA47-71CA-11E1-9E33-C80AA9429562:23",
				},
			},
			{
				PrimaryStatus: &MySQLPrimaryStatus{
					ExecutedGtidSet: "3E11FA47-71CA-11E1-9E33-C80AA9429562:23",
				},
			},
		}
		idx, err = GetLatestInstance(ctx, db, status)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*idx).Should(Equal(0))

		By("comparing the GTIDs")
		status = []MySQLInstanceStatus{
			{
				PrimaryStatus: &MySQLPrimaryStatus{
					ExecutedGtidSet: "3E11FA47-71CA-11E1-9E33-C80AA9429562:23",
				},
			},
			{
				PrimaryStatus: &MySQLPrimaryStatus{
					ExecutedGtidSet: "3E11FA47-71CA-11E1-9E33-C80AA9429562:21-57",
				},
			},
		}
		idx, err = GetLatestInstance(ctx, db, status)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(*idx).Should(Equal(1))

		By("comparing the inconsistent GTIDs")
		status = []MySQLInstanceStatus{
			{
				PrimaryStatus: &MySQLPrimaryStatus{
					ExecutedGtidSet: "3E11FA47-71CA-11E1-9E33-C80AA9429562:20-25",
				},
			},
			{
				PrimaryStatus: &MySQLPrimaryStatus{
					ExecutedGtidSet: "3E11FA47-71CA-11E1-9E33-C80AA9429562:21-57",
				},
			},
		}
		_, err = GetLatestInstance(ctx, db, status)
		Expect(err.Error()).Should(Equal("cannot compare gtids"))
	})
})