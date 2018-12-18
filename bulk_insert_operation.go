package ravendb

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Note: the implementation details are different from Java
// We take advantage of a pipe: a read end is passed as io.Reader
// to the request. A write end is what we use to write to the request.

var _ RavenCommand = &BulkInsertCommand{}

type BulkInsertCommand struct {
	RavenCommandBase

	_stream io.Reader

	_id int

	useCompression bool

	Result *http.Response
}

func NewBulkInsertCommand(id int, stream io.Reader, useCompression bool) *BulkInsertCommand {
	cmd := &BulkInsertCommand{
		RavenCommandBase: NewRavenCommandBase(),

		_stream:        stream,
		_id:            id,
		useCompression: useCompression,
	}
	return cmd
}

func (c *BulkInsertCommand) CreateRequest(node *ServerNode) (*http.Request, error) {
	url := node.GetUrl() + "/databases/" + node.GetDatabase() + "/bulk_insert?id=" + strconv.Itoa(c._id)
	// TODO: implement compression. It must be attached to the writer
	//message.setEntity(useCompression ? new GzipCompressingEntity(_stream) : _stream)
	return NewHttpPostReader(url, c._stream)
}

func (c *BulkInsertCommand) SetResponse(response []byte, fromCache bool) error {
	return NewNotImplementedException("Not implemented")
}

// TODO: port this. Currently send can't be over-written
/*
 CloseableHttpResponse send(CloseableHttpClient client, HttpRequestBase request) throws IOException {
	try {
		return super.send(client, request)
	} catch (Exception e) {
		_stream.errorOnRequestStart(e)
		throw e
	}
}
*/

type BulkInsertOperation struct {
	_generateEntityIDOnTheClient *generateEntityIDOnTheClient
	_requestExecutor             *RequestExecutor

	_bulkInsertExecuteTask *CompletableFuture

	_reader        *io.PipeReader
	_currentWriter *io.PipeWriter

	_first       bool
	_operationID int

	useCompression bool

	_concurrentCheck atomicInteger

	_conventions *DocumentConventions
	err          error

	Command *BulkInsertCommand
}

func NewBulkInsertOperation(database string, store *IDocumentStore) *BulkInsertOperation {
	re := store.GetRequestExecutorWithDatabase(database)
	f := func(entity interface{}) string {
		return re.GetConventions().GenerateDocumentID(database, entity)
	}

	reader, writer := io.Pipe()

	res := &BulkInsertOperation{
		_conventions:                 store.GetConventions(),
		_requestExecutor:             re,
		_generateEntityIDOnTheClient: newgenerateEntityIDOnTheClient(re.GetConventions(), f),
		_reader:                      reader,
		_currentWriter:               writer,
		_operationID:                 -1,
		_first:                       true,
	}
	return res
}

func (o *BulkInsertOperation) IsUseCompression() bool {
	return o.useCompression
}

func (o *BulkInsertOperation) SetUseCompression(useCompression bool) {
	o.useCompression = useCompression
}

func (o *BulkInsertOperation) throwBulkInsertAborted(e error, flushEx error) error {
	err := error(o.getExceptionFromOperation())
	if err == nil {
		err = e
	}
	if err == nil {
		err = flushEx
	}
	return NewBulkInsertAbortedException("Failed to execute bulk insert, error: %s", err)
}

func (o *BulkInsertOperation) getExceptionFromOperation() *BulkInsertAbortedException {
	stateRequest := NewGetOperationStateCommand(o._requestExecutor.GetConventions(), o._operationID)
	err := o._requestExecutor.ExecuteCommand(stateRequest)
	if err != nil {
		return nil // TODO: return an error?
	}

	if result, ok := stateRequest.Result["Result"]; ok {
		if result, ok := result.(ObjectNode); ok {
			typ, _ := jsonGetAsString(result, "$type")
			if strings.HasPrefix(typ, "Raven.Client.Documents.Operations.OperationExceptionResult") {
				errStr, _ := jsonGetAsString(result, "Error")
				return NewBulkInsertAbortedException(errStr)
			}
		}
	}
	return nil
}

func (o *BulkInsertOperation) WaitForID() error {
	if o._operationID != -1 {
		return nil
	}

	bulkInsertGetIDRequest := NewGetNextOperationIDCommand()
	o.err = o._requestExecutor.ExecuteCommand(bulkInsertGetIDRequest)
	if o.err != nil {
		return o.err
	}
	o._operationID = bulkInsertGetIDRequest.Result
	return nil
}

func (o *BulkInsertOperation) StoreWithID(entity interface{}, id string, metadata *MetadataAsDictionary) error {
	if !o._concurrentCheck.compareAndSet(0, 1) {
		return NewIllegalStateException("Bulk Insert Store methods cannot be executed concurrently.")
	}
	defer o._concurrentCheck.set(0)

	// early exit if we failed previously
	if o.err != nil {
		return o.err
	}

	err := bulkInsertOperationVerifyValidID(id)
	if err != nil {
		return err
	}
	o.err = o.WaitForID()
	if o.err != nil {
		return o.err
	}
	o.err = o.ensureCommand()
	if o.err != nil {
		return o.err
	}

	if o._bulkInsertExecuteTask.IsCompletedExceptionally() {
		_, err = o._bulkInsertExecuteTask.Get()
		panicIf(err == nil, "err should not be nil")
		return o.throwBulkInsertAborted(err, nil)
	}

	if metadata == nil {
		metadata = &MetadataAsDictionary{}
	}

	if !metadata.ContainsKey(Constants_Documents_Metadata_COLLECTION) {
		collection := o._requestExecutor.GetConventions().GetCollectionName(entity)
		if collection != "" {
			metadata.Put(Constants_Documents_Metadata_COLLECTION, collection)
		}
	}
	if !metadata.ContainsKey(Constants_Documents_Metadata_RAVEN_GO_TYPE) {
		goType := o._requestExecutor.GetConventions().GetGoTypeName(entity)
		if goType != "" {
			metadata.Put(Constants_Documents_Metadata_RAVEN_GO_TYPE, goType)
		}
	}

	documentInfo := &documentInfo{}
	documentInfo.metadataInstance = metadata
	jsNode := convertEntityToJSON(entity, documentInfo)

	var b bytes.Buffer
	if o._first {
		b.WriteByte('[')
		o._first = false
	} else {
		b.WriteByte(',')
	}
	m := map[string]interface{}{}
	m["Id"] = id
	m["Type"] = "PUT"
	m["Document"] = jsNode

	d, err := jsonMarshal(m)
	if err != nil {
		return err
	}
	b.Write(d)

	_, o.err = o._currentWriter.Write(b.Bytes())
	if o.err != nil {
		err = o.getExceptionFromOperation()
		if err != nil {
			o.err = err
			return o.err
		}
		// TODO:
		//o.err = o.throwOnUnavailableStream()
		return o.err
	}
	return o.err
}

func (o *BulkInsertOperation) ensureCommand() error {
	if o.Command != nil {
		return nil
	}
	bulkCommand := NewBulkInsertCommand(o._operationID, o._reader, o.useCompression)
	panicIf(o._bulkInsertExecuteTask != nil, "already started _bulkInsertExecuteTask")
	o._bulkInsertExecuteTask = NewCompletableFuture()
	go func() {
		err := o._requestExecutor.ExecuteCommand(bulkCommand)
		if err != nil {
			o._bulkInsertExecuteTask.CompleteExceptionally(err)
		} else {
			o._bulkInsertExecuteTask.Complete(nil)
		}
	}()

	o.Command = bulkCommand
	return nil
}

func (o *BulkInsertOperation) Abort() error {
	if o._operationID == -1 {
		return nil // nothing was done, nothing to kill
	}

	err := o.WaitForID()
	if err != nil {
		return err
	}

	command := NewKillOperationCommand(strconv.Itoa(o._operationID))
	err = o._requestExecutor.ExecuteCommand(command)
	//o._currentWriter.Close()
	if err != nil {
		return NewBulkInsertAbortedException("%s", "Unable to kill ths bulk insert operation, because it was not found on the server.")
	}
	o._currentWriter.CloseWithError(NewBulkInsertAbortedException("killed operation"))
	return nil
}

func (o *BulkInsertOperation) Close() error {
	if o._operationID == -1 {
		// closing without calling a single Store.
		return nil
	}

	d := []byte{']'}
	_, err := o._currentWriter.Write(d)
	errClose := o._currentWriter.Close()
	if o._bulkInsertExecuteTask != nil {
		_, err2 := o._bulkInsertExecuteTask.Get()
		if err2 != nil && err == nil {
			err = o.throwBulkInsertAborted(err, errClose)
		}
	}

	if err != nil {
		o.err = err
		return err
	}
	return nil
}

func (o *BulkInsertOperation) Store(entity interface{}) (string, error) {
	return o.StoreWithMetadata(entity, nil)
}

func (o *BulkInsertOperation) StoreWithMetadata(entity interface{}, metadata *MetadataAsDictionary) (string, error) {
	var id string
	if metadata == nil || !metadata.ContainsKey(Constants_Documents_Metadata_ID) {
		id = o.GetID(entity)
	} else {
		idVal, ok := metadata.Get(Constants_Documents_Metadata_ID)
		panicIf(!ok, "didn't find %s key in meatadata", Constants_Documents_Metadata_ID)
		id = idVal.(string)
	}

	return id, o.StoreWithID(entity, id, metadata)
}

func (o *BulkInsertOperation) GetID(entity interface{}) string {
	idRef, ok := o._generateEntityIDOnTheClient.tryGetIDFromInstance(entity)
	if ok {
		return idRef
	}

	idRef = o._generateEntityIDOnTheClient.generateDocumentKeyForStorage(entity)

	// set id property if it was null
	o._generateEntityIDOnTheClient.trySetIdentity(entity, idRef)
	return idRef
}

func (o *BulkInsertOperation) throwOnUnavailableStream(id string, innerEx error) error {
	// TODO: not sure how this translates
	//_streamExposerContent.errorOnProcessingRequest(new BulkInsertAbortedException("Write to stream failed at document with id " + id, innerEx))

	_, err := o._bulkInsertExecuteTask.Get()
	if err != nil {
		return ExceptionsUtils_unwrapException(err)
	}
	return nil
}

func bulkInsertOperationVerifyValidID(id string) error {
	if stringIsEmpty(id) {
		return NewIllegalStateException("Document id must have a non empty value")
	}

	if strings.HasSuffix(id, "|") {
		return NewUnsupportedOperationException("Document ids cannot end with '|', but was called with %s", id)
	}
	return nil
}
