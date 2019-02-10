package ravendb

import (
	"fmt"
	"reflect"
	"strings"
)

type LoadOperation struct {
	_session *InMemoryDocumentSessionOperations

	_ids                []string
	_includes           []string
	_idsToCheckOnServer []string
}

func NewLoadOperation(_session *InMemoryDocumentSessionOperations) *LoadOperation {
	return &LoadOperation{
		_session: _session,
	}
}

func (o *LoadOperation) createRequest() (*GetDocumentsCommand, error) {
	if len(o._idsToCheckOnServer) == 0 {
		return nil, nil
	}

	if o._session.checkIfIdAlreadyIncluded(o._ids, o._includes) {
		return nil, nil
	}

	if err := o._session.incrementRequestCount(); err != nil {
		return nil, err
	}

	return NewGetDocumentsCommand(o._idsToCheckOnServer, o._includes, false)
}

func (o *LoadOperation) byID(id string) *LoadOperation {
	if id == "" {
		return o
	}

	if o._ids == nil {
		o._ids = []string{id}
	}

	if o._session.IsLoadedOrDeleted(id) {
		return o
	}

	o._idsToCheckOnServer = append(o._idsToCheckOnServer, id)
	return o
}

func (o *LoadOperation) withIncludes(includes []string) *LoadOperation {
	o._includes = includes
	return o
}

func (o *LoadOperation) byIds(ids []string) *LoadOperation {
	o._ids = stringArrayCopy(ids)

	seen := map[string]struct{}{}
	for _, id := range ids {
		if id == "" {
			continue
		}
		idl := strings.ToLower(id)
		if _, ok := seen[idl]; ok {
			continue
		}
		seen[idl] = struct{}{}
		o.byID(id)
	}
	return o
}

func (o *LoadOperation) getDocument(result interface{}) error {
	return o.getDocumentWithID(result, o._ids[0])
}

func (o *LoadOperation) getDocumentWithID(result interface{}, id string) error {
	if id == "" {
		// TODO: should return default value?
		//return ErrNotFound
		return nil
	}

	if o._session.IsDeleted(id) {
		// TODO: return ErrDeleted?
		//return ErrNotFound
		return nil
	}

	doc := o._session.documentsByID.getValue(id)
	if doc == nil {
		doc = o._session.includedDocumentsByID[id]
	}
	if doc == nil {
		//return ErrNotFound
		return nil
	}

	return o._session.TrackEntityInDocumentInfo(result, doc)
}

var stringType = reflect.TypeOf("")

// TODO: also handle a pointer to a map?
func (o *LoadOperation) getDocuments(results interface{}) error {
	// results must be map[string]*struct
	//fmt.Printf("LoadOperation.getDocuments: results type: %T\n", results)
	m := reflect.ValueOf(results)
	if m.Type().Kind() != reflect.Map {
		return fmt.Errorf("results should be a map[string]*struct, is %s. tp: %s", m.Type().String(), m.Type().String())
	}
	mapKeyType := m.Type().Key()
	if mapKeyType != stringType {
		return fmt.Errorf("results should be a map[string]*struct, is %s. tp: %s", m.Type().String(), m.Type().String())
	}
	mapElemPtrType := m.Type().Elem()
	if mapElemPtrType.Kind() != reflect.Ptr {
		return fmt.Errorf("results should be a map[string]*struct, is %s. tp: %s", m.Type().String(), m.Type().String())
	}
	mapElemType := mapElemPtrType.Elem()
	if mapElemType.Kind() != reflect.Struct {
		return fmt.Errorf("results should be a map[string]*struct, is %s. tp: %s", m.Type().String(), m.Type().String())
	}

	uniqueIds := stringArrayCopy(o._ids)
	stringArrayRemove(&uniqueIds, "")
	uniqueIds = stringArrayRemoveDuplicatesNoCase(uniqueIds)
	for _, id := range uniqueIds {
		v := reflect.New(mapElemPtrType).Interface()
		err := o.getDocumentWithID(v, id)
		if err != nil {
			return err
		}
		key := reflect.ValueOf(id)
		v2 := reflect.ValueOf(v).Elem() // convert *<type> to <type>
		m.SetMapIndex(key, v2)
	}

	return nil
}

func (o *LoadOperation) setResult(result *GetDocumentsResult) {
	if result == nil {
		return
	}

	o._session.registerIncludes(result.Includes)

	results := result.Results
	for _, document := range results {
		// TODO: Java also does document.isNull()
		if document == nil {
			continue
		}
		newDocumentInfo := getNewDocumentInfo(document)
		o._session.documentsByID.add(newDocumentInfo)
	}

	o._session.registerMissingIncludes(result.Results, result.Includes, o._includes)
}
