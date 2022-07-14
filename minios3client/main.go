package main

import (
	"context"
	"fmt"
	"log"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var endpoint string = "127.0.0.1:9000"

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
	adminUser := client("L4G8GAN5X1EYNI8VJ6YV", "Ti8gFflHZALYN5iFrPmzO7g6oGUVjqUNDJnIMbOM")
	fmt.Println(adminUser.ListBuckets(context.Background()))
}
