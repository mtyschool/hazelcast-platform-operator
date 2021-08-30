package controllers

import (
	"github.com/hazelcast/hazelcast-enterprise-operator/api/v1alpha1"
	"github.com/hazelcast/hazelcast-go-client/cluster"
	hzTypes "github.com/hazelcast/hazelcast-go-client/types"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"time"
)

var _ = Describe("Hazelcast status", func() {
	const (
		timeout  = time.Second * 5
		interval = time.Millisecond * 250
	)

	var hzClient = HazelcastClient{
		MemberMap:           make(map[string]bool),
		memberEventsChannel: make(chan event.GenericEvent),
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      "hazelcast",
		},
	}

	Context("Hazelcast membership listener", func() {
		It("Should add new member to the map", func() {
			stateChanged := cluster.MembershipStateChanged{
				Member: cluster.MemberInfo{
					Address: cluster.NewAddress("127.0.0.1", 5701),
					UUID:    hzTypes.NewUUID(),
				},
				State: cluster.MembershipStateAdded,
			}
			go getStatusUpdateListener(hzClient)(stateChanged)

			Eventually(func() bool {
				return hzClient.MemberMap[stateChanged.Member.String()]
			}, timeout, interval).Should(BeTrue())

			Eventually(func() event.GenericEvent {
				select {
				case e := <-hzClient.memberEventsChannel:
					return e
				default:
					return event.GenericEvent{}
				}
			}).Should(Equal(event.GenericEvent{
				Object: &v1alpha1.Hazelcast{ObjectMeta: metav1.ObjectMeta{
					Namespace: hzClient.NamespacedName.Namespace,
					Name:      hzClient.NamespacedName.Name,
				}}}))

		})
	})
})
