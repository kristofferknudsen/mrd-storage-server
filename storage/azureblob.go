package storage

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/url"
	"path"
	"strings"

	"github.com/Azure/azure-storage-blob-go/azblob"
	"github.com/ismrmrd/mrd-storage-server/core"
)

var (
	ErrInvalidConnectionString = errors.New("invalid connection string")
)

type azureBlobStore struct {
	containerUrl azblob.ContainerURL
}

func NewAzureBlobStore(connectionString string) (core.BlobStore, error) {
	connectionStringProperties := make(map[string]string)
	for _, p := range strings.Split(connectionString, ";") {
		if p == "" {
			continue
		}
		tokens := strings.SplitN(p, "=", 2)
		if len(tokens) != 2 {
			return nil, ErrInvalidConnectionString
		}

		connectionStringProperties[tokens[0]] = tokens[1]
	}

	credential, err := azblob.NewSharedKeyCredential(connectionStringProperties["AccountName"], connectionStringProperties["AccountKey"])
	if err != nil {
		return nil, ErrInvalidConnectionString
	}

	pipeline := azblob.NewPipeline(credential, azblob.PipelineOptions{})
	endpoint, err := url.Parse(connectionStringProperties["BlobEndpoint"])
	if endpoint == nil || err != nil {
		return nil, ErrInvalidConnectionString
	}

	serviceUrl := azblob.NewServiceURL(*endpoint, pipeline)

	containerUrl := serviceUrl.NewContainerURL("mrd-storage-server")
	_, err = containerUrl.Create(context.Background(), azblob.Metadata{}, azblob.PublicAccessNone)
	if err != nil {
		if storageErr, ok := err.(azblob.StorageError); !ok || storageErr.ServiceCode() != azblob.ServiceCodeContainerAlreadyExists {
			return nil, err
		}
	}

	return &azureBlobStore{containerUrl: containerUrl}, nil
}

func (s *azureBlobStore) SaveBlob(ctx context.Context, contents io.Reader, key core.BlobKey) error {
	blobUrl := s.containerUrl.NewBlockBlobURL(blobName(key))
	_, err := azblob.UploadStreamToBlockBlob(context.Background(), contents, blobUrl, azblob.UploadStreamToBlockBlobOptions{})
	return err
}

func (s *azureBlobStore) ReadBlob(ctx context.Context, writer io.Writer, key core.BlobKey) error {
	blobUrl := s.containerUrl.NewBlockBlobURL(blobName(key))
	downloadResponse, err := blobUrl.Download(ctx, 0, azblob.CountToEnd, azblob.BlobAccessConditions{}, false, azblob.ClientProvidedKeyOptions{})
	if err != nil {
		if storageErr, ok := err.(azblob.StorageError); ok && storageErr.ServiceCode() == azblob.ServiceCodeBlobNotFound {
			return core.ErrBlobNotFound
		} else {
			return err
		}
	}

	bodyStream := downloadResponse.Body(azblob.RetryReaderOptions{MaxRetryRequests: 20})
	_, err = io.Copy(writer, bodyStream)
	return err
}

func (s *azureBlobStore) DeleteBlob(ctx context.Context, key core.BlobKey) error {
	blobUrl := s.containerUrl.NewBlockBlobURL(blobName(key))

	if _, err := blobUrl.Delete(ctx, azblob.DeleteSnapshotsOptionNone, azblob.BlobAccessConditions{}); err != nil {
		if storageErr, ok := err.(azblob.StorageError); !ok || storageErr.ServiceCode() != azblob.ServiceCodeBlobNotFound {
			return err
		}
	}

	return nil
}

func blobName(key core.BlobKey) string {
	// make sure we don't have file names / or .. or anything like that
	encodedSubject := base64.RawURLEncoding.EncodeToString([]byte(key.Subject))
	return path.Join(encodedSubject, key.Id.String())
}
