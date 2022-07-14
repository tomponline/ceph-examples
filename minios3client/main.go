package main

import (
	"log"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var endpoint string = "10.96.75.11:8080"

func client(accessKey string, accessSecret string) *minio.Client {
	// Initialize minio client object.
	minioClient, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, accessSecret, ""),
		Secure: false,
	})
	if err != nil {
		log.Fatalln(err)
	}

	return minioClient
}

func main() {

}
