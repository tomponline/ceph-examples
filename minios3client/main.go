package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var endpoint string = "127.0.0.1:9000"

func client(accessKey string, accessSecret string) *minio.Client {
	// Initialize minio client object.
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:     credentials.NewStaticV4(accessKey, accessSecret, ""),
		Secure:    true,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	})
	if err != nil {
		log.Fatalln(err)
	}

	return minioClient
}

func main() {
	adminUser := client("MZc88riW5mXayhlO", "xDjKf9C4VkoTuSMu4Fss5D0DELC37X2W")
	fmt.Println(adminUser.ListBuckets(context.Background()))

	objectCh := adminUser.ListObjects(context.Background(), "foo", minio.ListObjectsOptions{})
	for object := range objectCh {
		if object.Err != nil {
			fmt.Println(object.Err)
			return
		}
		fmt.Println(object)
	}
}
