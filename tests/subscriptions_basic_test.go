package tests

import (
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ravendb/ravendb-go-client"
	"github.com/stretchr/testify/assert"
)

const (
	_reasonableWaitTime = time.Second * 5 // TODO: is it 60 seconds in Java?
)

func subscriptionsBasic_canDeleteSubscription(t *testing.T, driver *RavenTestDriver) {
	var err error
	store := driver.getDocumentStoreMust(t)
	defer store.Close()

	id1, err := store.Subscriptions.CreateForType(reflect.TypeOf(&User{}), nil, "")
	assert.NoError(t, err)
	id2, err := store.Subscriptions.CreateForType(reflect.TypeOf(&User{}), nil, "")
	assert.NoError(t, err)

	subscriptions, err := store.Subscriptions.GetSubscriptions(0, 5, "")
	assert.NoError(t, err)
	assert.Equal(t, len(subscriptions), 2)
	err = store.Subscriptions.Delete(id1, "")
	assert.NoError(t, err)
	err = store.Subscriptions.Delete(id2, "")
	assert.NoError(t, err)

	subscriptions, err = store.Subscriptions.GetSubscriptions(0, 5, "")
	assert.NoError(t, err)
	assert.Equal(t, len(subscriptions), 0)
}

func subscriptionsBasic_shouldThrowWhenOpeningNoExisingSubscription(t *testing.T, driver *RavenTestDriver) {
	store := driver.getDocumentStoreMust(t)
	defer store.Close()

	clazz := reflect.TypeOf(&map[string]interface{}{})
	opts, err := ravendb.NewSubscriptionWorkerOptions("1")
	assert.NoError(t, err)
	subscription, err := store.Subscriptions.GetSubscriptionWorker(clazz, opts, "")
	assert.NoError(t, err)
	fn := func(x *ravendb.SubscriptionBatch) error {
		// no-op
		return nil
	}

	res, err := subscription.Run(fn)
	assert.NoError(t, err)
	_, err = res.Get()
	assert.NotNil(t, err)
	_, ok := err.(*ravendb.SubscriptionDoesNotExistError)
	assert.True(t, ok)

	err = subscription.Close()
	assert.NoError(t, err)
}

func subscriptionsBasic_shouldThrowOnAttemptToOpenAlreadyOpenedSubscription(t *testing.T, driver *RavenTestDriver) {
	store := driver.getDocumentStoreMust(t)
	defer store.Close()

	id, err := store.Subscriptions.CreateForType(reflect.TypeOf(&User{}), nil, "")
	assert.NoError(t, err)

	{
		clazz := reflect.TypeOf(map[string]interface{}{})
		opts, err := ravendb.NewSubscriptionWorkerOptions(id)
		assert.NoError(t, err)
		subscription, err := store.Subscriptions.GetSubscriptionWorker(clazz, opts, "")
		assert.NoError(t, err)

		{
			session, err := store.OpenSession()
			assert.NoError(t, err)
			err = session.Store(&User{})
			assert.NoError(t, err)
			err = session.SaveChanges()
			assert.NoError(t, err)

			session.Close()
		}

		semaphore := make(chan bool)
		fn := func(x *ravendb.SubscriptionBatch) error {
			semaphore <- true
			return nil
		}
		_, err = subscription.Run(fn)
		assert.NoError(t, err)

		select {
		case <-semaphore:
			// no-op
		case <-time.After(_reasonableWaitTime):
			// no-op
		}

		options2, err := ravendb.NewSubscriptionWorkerOptions(id)
		assert.NoError(t, err)
		options2.Strategy = ravendb.SubscriptionOpeningStrategyOpenIfFree

		{
			secondSubscription, err := store.Subscriptions.GetSubscriptionWorker(clazz, options2, "")
			assert.NoError(t, err)
			fn := func(x *ravendb.SubscriptionBatch) error {
				// no-op
				return nil
			}
			future, err := secondSubscription.Run(fn)
			assert.NoError(t, err)
			_, err = future.Get()
			_, ok := err.(*ravendb.SubscriptionInUseError)
			assert.True(t, ok)

			err = secondSubscription.Close()
			assert.NoError(t, err)
		}

		err = subscription.Close()
		assert.NoError(t, err)
	}

}

func subscriptionsBasic_shouldStreamAllDocumentsAfterSubscriptionCreation(t *testing.T, driver *RavenTestDriver) {
	var err error
	store := driver.getDocumentStoreMust(t)
	defer store.Close()

	{
		session := openSessionMust(t, store)

		user1 := &User{
			Age: 31,
		}
		err = session.StoreWithID(user1, "users/1")
		assert.NoError(t, err)

		user2 := &User{
			Age: 27,
		}
		err = session.StoreWithID(user2, "users/12")
		assert.NoError(t, err)

		user3 := &User{
			Age: 25,
		}
		err = session.StoreWithID(user3, "users/3")
		assert.NoError(t, err)

		err = session.SaveChanges()
		assert.NoError(t, err)

		session.Close()
	}

	id, err := store.Subscriptions.CreateForType(reflect.TypeOf(&User{}), nil, "")
	assert.NoError(t, err)

	{
		opts, err := ravendb.NewSubscriptionWorkerOptions(id)
		assert.NoError(t, err)
		clazz := reflect.TypeOf(&User{})
		subscription, err := store.Subscriptions.GetSubscriptionWorker(clazz, opts, "")
		assert.NoError(t, err)

		keys := make(chan string)
		ages := make(chan int)

		fn := func(batch *ravendb.SubscriptionBatch) error {
			// Note: important that done in two separate passes
			for _, item := range batch.Items {
				keys <- item.ID
			}

			for _, item := range batch.Items {
				v, err := item.GetResult()
				assert.NoError(t, err)
				u := v.(*User)
				ages <- u.Age
			}
			return nil
		}
		_, err = subscription.Run(fn)
		assert.NoError(t, err)

		getNextKey := func() string {
			select {
			case v := <-keys:
				return v
			case <-time.After(_reasonableWaitTime):
				// no-op
			}
			return ""
		}
		key := getNextKey()
		assert.Equal(t, key, "users/1")
		key = getNextKey()
		assert.Equal(t, key, "users/12")
		key = getNextKey()
		assert.Equal(t, key, "users/3")

		getNextAge := func() int {
			select {
			case v := <-ages:
				return v
			case <-time.After(_reasonableWaitTime):
				// no-op
			}
			return 0
		}
		age := getNextAge()
		assert.Equal(t, age, 31)
		age = getNextAge()
		assert.Equal(t, age, 27)
		age = getNextAge()
		assert.Equal(t, age, 25)

		err = subscription.Close()
		assert.NoError(t, err)
	}
}

func subscriptionsBasic_shouldSendAllNewAndModifiedDocs(t *testing.T, driver *RavenTestDriver) {
	var err error
	store := driver.getDocumentStoreMust(t)
	defer store.Close()

	id, err := store.Subscriptions.CreateForType(reflect.TypeOf(&User{}), nil, "")
	assert.NoError(t, err)

	{
		opts, err := ravendb.NewSubscriptionWorkerOptions(id)
		assert.NoError(t, err)
		clazz := reflect.TypeOf(map[string]interface{}{})
		subscription, err := store.Subscriptions.GetSubscriptionWorker(clazz, opts, "")
		assert.NoError(t, err)

		names := make(chan string)

		processBatch := func(batch *ravendb.SubscriptionBatch) error {
			for _, item := range batch.Items {
				v, err := item.GetResult()
				assert.NoError(t, err)
				m := v.(map[string]interface{})
				name := m["name"].(string)
				names <- name
			}
			return nil
		}

		{
			session := openSessionMust(t, store)

			user := &User{}
			user.setName("James")
			err = session.StoreWithID(user, "users/1")
			assert.NoError(t, err)

			err = session.SaveChanges()
			assert.NoError(t, err)

			session.Close()
		}

		_, err = subscription.Run(processBatch)
		assert.NoError(t, err)

		getNextName := func() string {
			select {
			case v := <-names:
				return v
			case <-time.After(_reasonableWaitTime):
				// no-op
			}
			return ""
		}

		name := getNextName()
		assert.Equal(t, name, "James")

		{
			session := openSessionMust(t, store)

			user := &User{}
			user.setName("Adam")
			err = session.StoreWithID(user, "users/12")
			assert.NoError(t, err)

			err = session.SaveChanges()
			assert.NoError(t, err)

			session.Close()
		}

		name = getNextName()
		assert.Equal(t, name, "Adam")

		//Thread.sleep(15000); // test with sleep - let few heartbeats come to us - commented out for CI

		{
			session := openSessionMust(t, store)

			user := &User{}
			user.setName("David")
			err = session.StoreWithID(user, "users/1")
			assert.NoError(t, err)

			err = session.SaveChanges()
			assert.NoError(t, err)

			session.Close()
		}

		name = getNextName()
		assert.Equal(t, name, "David")

		err = subscription.Close()
		assert.NoError(t, err)
	}
}

func subscriptionsBasic_shouldRespectMaxDocCountInBatch(t *testing.T, driver *RavenTestDriver) {
	var err error
	store := driver.getDocumentStoreMust(t)
	defer store.Close()

	{
		session := openSessionMust(t, store)

		for i := 0; i < 100; i++ {
			err = session.Store(&Company{})
			assert.NoError(t, err)
		}

		err = session.SaveChanges()
		assert.NoError(t, err)

		session.Close()
	}

	clazz := reflect.TypeOf(&Company{})
	id, err := store.Subscriptions.CreateForType(clazz, nil, "")
	assert.NoError(t, err)

	options, err := ravendb.NewSubscriptionWorkerOptions(id)
	assert.NoError(t, err)
	options.MaxDocsPerBatch = 25

	{
		clazz = reflect.TypeOf(map[string]interface{}{})
		subscriptionWorker, err := store.Subscriptions.GetSubscriptionWorker(clazz, options, "")
		assert.NoError(t, err)

		var totalItems int32
		semaphore := make(chan bool)
		processBatch := func(batch *ravendb.SubscriptionBatch) error {
			n := len(batch.Items)
			assert.True(t, n <= 25)
			total := atomic.AddInt32(&totalItems, int32(n))
			if total == 100 {
				semaphore <- true
			}
			return nil
		}
		_, err = subscriptionWorker.Run(processBatch)
		assert.NoError(t, err)

		select {
		case <-semaphore:
		// no-op
		case <-time.After(_reasonableWaitTime):
			assert.True(t, false)
		}
		subscriptionWorker.Close()
	}
}

func subscriptionsBasic_shouldRespectCollectionCriteria(t *testing.T, driver *RavenTestDriver) {
}

func subscriptionsBasic_willAcknowledgeEmptyBatches(t *testing.T, driver *RavenTestDriver) {
}

func subscriptionsBasic_canReleaseSubscription(t *testing.T, driver *RavenTestDriver) {
}

func subscriptionsBasic_shouldPullDocumentsAfterBulkInsert(t *testing.T, driver *RavenTestDriver) {
}

func subscriptionsBasic_shouldStopPullingDocsAndCloseSubscriptionOnSubscriberErrorByDefault(t *testing.T, driver *RavenTestDriver) {
}

func subscriptionsBasic_canSetToIgnoreSubscriberErrors(t *testing.T, driver *RavenTestDriver) {
}

func subscriptionsBasic_ravenDB_3452_ShouldStopPullingDocsIfReleased(t *testing.T, driver *RavenTestDriver) {
}

func subscriptionsBasic_ravenDB_3453_ShouldDeserializeTheWholeDocumentsAfterTypedSubscription(t *testing.T, driver *RavenTestDriver) {
}

func subscriptionsBasic_disposingOneSubscriptionShouldNotAffectOnNotificationsOfOthers(t *testing.T, driver *RavenTestDriver) {
}

func TestSubscriptionsBasic(t *testing.T) {
	t.Parallel()

	driver := createTestDriver(t)
	destroy := func() { destroyDriver(t, driver) }
	defer recoverTest(t, destroy)

	// matches order of Java tests

	// TODO: arrange in Java order

	if true {
		subscriptionsBasic_canDeleteSubscription(t, driver)
		subscriptionsBasic_shouldThrowWhenOpeningNoExisingSubscription(t, driver)
		subscriptionsBasic_shouldThrowOnAttemptToOpenAlreadyOpenedSubscription(t, driver)
		subscriptionsBasic_shouldStreamAllDocumentsAfterSubscriptionCreation(t, driver)
		subscriptionsBasic_shouldSendAllNewAndModifiedDocs(t, driver)
		subscriptionsBasic_shouldRespectMaxDocCountInBatch(t, driver)
	}

	/*
		subscriptionsBasic_shouldRespectCollectionCriteria(t, driver)
		subscriptionsBasic_willAcknowledgeEmptyBatches(t, driver)
		subscriptionsBasic_canReleaseSubscription(t, driver)
		subscriptionsBasic_shouldPullDocumentsAfterBulkInsert(t, driver)
		subscriptionsBasic_shouldStopPullingDocsAndCloseSubscriptionOnSubscriberErrorByDefault(t, driver)
		subscriptionsBasic_canSetToIgnoreSubscriberErrors(t, driver)
		subscriptionsBasic_ravenDB_3452_ShouldStopPullingDocsIfReleased(t, driver)
		subscriptionsBasic_ravenDB_3453_ShouldDeserializeTheWholeDocumentsAfterTypedSubscription(t, driver)
		subscriptionsBasic_disposingOneSubscriptionShouldNotAffectOnNotificationsOfOthers(t, driver)
	*/
}