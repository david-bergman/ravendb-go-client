package tests

import (
	"github.com/ravendb/ravendb-go-client"
	"github.com/ravendb/ravendb-go-client/examples/northwind"
	"github.com/stretchr/testify/assert"
	"reflect"
	"testing"
)

func createNorthwindDatabase(t *testing.T, driver *RavenTestDriver, store *ravendb.DocumentStore) {
	sampleData := ravendb.NewCreateSampleDataOperation()
	err := store.Maintenance().Send(sampleData)
	must(err)

	err = driver.waitForIndexing(store, store.GetDatabase(), 0)
	must(err)
}

func goNorthwindEmployeeLoad(t *testing.T, driver *RavenTestDriver) {
	var err error
	store := driver.getDocumentStoreMust(t)
	defer store.Close()

	createNorthwindDatabase(t, driver, store)

	session, err := store.OpenSession("")
	assert.NoError(t, err)
	defer session.Close()

	var e *northwind.Employee
	err = session.Load(&e, "employees/7-A")
	assert.NoError(t, err)

	var results []*northwind.Employee
	args := &ravendb.StartsWithArgs{
		StartsWith: "employees/",
	}
	err = session.LoadStartingWith(&results, args)
	assert.NoError(t, err)
	assert.True(t, len(results) > 5) // it's 9 currently
}

func goNorthwindWhereBetween(t *testing.T, driver *RavenTestDriver) {
	var err error
	store := driver.getDocumentStoreMust(t)
	defer store.Close()

	createNorthwindDatabase(t, driver, store)

	session, err := store.OpenSession("")
	assert.NoError(t, err)
	defer session.Close()

	tp := reflect.TypeOf(&northwind.Order{})
	q := session.QueryCollectionForType(tp)
	q = q.WhereBetween("Freight", 11, 13)

	var results []*northwind.Order
	err = q.GetResults(&results)
	assert.NoError(t, err)

	assert.True(t, len(results) > 5) // it's 35 currently
}

// test that Single()/First()/GetResults() validate early type of result
// https://github.com/ravendb/ravendb-go-client/issues/146
func goNorthwindIssue146(t *testing.T, driver *RavenTestDriver) {
	var err error
	store := driver.getDocumentStoreMust(t)
	defer store.Close()

	createNorthwindDatabase(t, driver, store)

	session, err := store.OpenSession("")
	assert.NoError(t, err)
	defer session.Close()

	{
		tp := reflect.TypeOf(&northwind.Employee{})
		q := session.QueryCollectionForType(tp)
		q = q.Where("ID", "=", "employees/1-A")
		err = q.Single(nil)
		_, ok := err.(*ravendb.IllegalArgumentError)
		assert.True(t, ok)
	}

	{
		tp := reflect.TypeOf(&northwind.Employee{})
		q := session.QueryCollectionForType(tp)
		q = q.Where("ID", "=", "employees/1-A")
		var results []*northwind.Order
		err = q.Single(&results)
		_, ok := err.(*ravendb.IllegalArgumentError)
		assert.True(t, ok)
	}

	{
		tp := reflect.TypeOf(&northwind.Employee{})
		q := session.QueryCollectionForType(tp)
		q = q.Where("ID", "=", "employees/1-A")
		err = q.First(nil)
		_, ok := err.(*ravendb.IllegalArgumentError)
		assert.True(t, ok)
	}

	{
		tp := reflect.TypeOf(&northwind.Employee{})
		q := session.QueryCollectionForType(tp)
		q = q.Where("ID", "=", "employees/1-A")
		var results []*northwind.Order
		err = q.First(&results)
		_, ok := err.(*ravendb.IllegalArgumentError)
		assert.True(t, ok)
	}

	{
		tp := reflect.TypeOf(&northwind.Employee{})
		q := session.QueryCollectionForType(tp)
		err = q.GetResults(nil)
		_, ok := err.(*ravendb.IllegalArgumentError)
		assert.True(t, ok)
	}

	{
		tp := reflect.TypeOf(&northwind.Employee{})
		q := session.QueryCollectionForType(tp)

		var results *northwind.Order
		err = q.GetResults(&results)
		_, ok := err.(*ravendb.IllegalArgumentError)
		assert.True(t, ok)
	}

}

func TestGoNorthwind(t *testing.T) {
	driver := createTestDriver(t)
	destroy := func() { destroyDriver(t, driver) }
	defer recoverTest(t, destroy)

	goNorthwindEmployeeLoad(t, driver)
	goNorthwindWhereBetween(t, driver)
	goNorthwindIssue146(t, driver)
}
