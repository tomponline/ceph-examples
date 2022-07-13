package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var endpoint string = "10.96.75.11:8080"

// Key is an S3 access key.
type Key struct {
	User      string `json:"user"`
	AccessKey string `json:"access_key"`
	SecretKey string `json:"secret_key"`
}

func removeUser(user string) error {
	cmd := exec.Command("radosgw-admin", "user", "rm", "--purge-data", fmt.Sprintf("--uid=%s", user))
	_, err := cmd.Output()

	if exiterr, ok := err.(*exec.ExitError); ok {
		return fmt.Errorf(string(exiterr.Stderr))
	}

	return err
}

func addUser(user string, buckets int) (*Key, error) {
	cmd := exec.Command("radosgw-admin", "user", "create", fmt.Sprintf("--max-buckets=%d", buckets), fmt.Sprintf("--display-name=%s", user), fmt.Sprintf("--uid=%s", user))
	buf, err := cmd.Output()

	if exiterr, ok := err.(*exec.ExitError); ok {
		return nil, fmt.Errorf(string(exiterr.Stderr))
	}

	if err != nil {
		return nil, err
	}

	info := struct {
		Keys []Key `json:"keys"`
	}{}

	err = json.Unmarshal(buf, &info)
	if err != nil {
		return nil, err
	}

	return &info.Keys[0], nil
}

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

func setBucketPolicy(client *minio.Client, allowAuthGet bool, allowAnonymousGet bool) error {
	otherAccess := ""
	if allowAnonymousGet {
		// Allow all users (including anonymous) to GET objects in bucket.
		otherAccess = `,{
				"Effect": "Allow",
				"Principal": "*",
				"Action": [
					"s3:GetObject",
					"s3:GetObjectVersion"
				],
				"Resource": [
					"arn:aws:s3:::mybucket/*"
				]
			}`
	} else if allowAuthGet {
		// Allow the testread user to GET objects in bucket.
		otherAccess = `,{
				"Effect": "Allow",
				"Principal": {
					"AWS": ["arn:aws:iam:::user/testread"]
				},
				"Action": [
					"s3:GetObject",
					"s3:GetObjectVersion"
				],
				"Resource": [
					"arn:aws:s3:::mybucket/*"
				]
			}`
	}

	// The default bucket policy just allows the testwrite user to GET/PUT/DELETE objects in bucket.
	policy := fmt.Sprintf(`{
		"Version": "2012-10-17",
		"Statement": [{
				"Effect": "Allow",
				"Principal": {
					"AWS": ["arn:aws:iam:::user/testwrite"]
				},
				"Action": [
					"s3:GetObject",
					"s3:GetObjectVersion",
					"s3:PutObject",
					"s3:DeleteObject"
				],
				"Resource": [
					"arn:aws:s3:::mybucket/*"
				]
			}%s
		]
	}`, otherAccess)

	fmt.Println(policy)

	return client.SetBucketPolicy(context.Background(), "mybucket", policy)
}

func putObject(client *minio.Client, bucket string) error {
	file, err := os.Open("upload.jpg")
	if err != nil {
		return err
	}

	defer file.Close()

	fileStat, err := file.Stat()
	if err != nil {
		return err
	}

	_, err = client.PutObject(context.Background(), bucket, "myobject", file, fileStat.Size(), minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})

	return err
}

func removeObject(client *minio.Client, bucket string) error {
	return client.RemoveObject(context.Background(), bucket, "myobject", minio.RemoveObjectOptions{ForceDelete: true})
}

func getObject(client *minio.Client, bucket string) error {
	object, err := client.GetObject(context.Background(), bucket, "myobject", minio.GetObjectOptions{})
	if err != nil {
		return err
	}

	file := "s3-local-file.jpg"
	os.Remove(file)
	localFile, err := os.Create(file)
	if err != nil {
		return err
	}

	_, err = io.Copy(localFile, object)

	return err
}

func getObjectAnonymous(bucket string) error {
	resp, err := http.Get(fmt.Sprintf("http://%s/%s/myobject", endpoint, bucket))
	if err != nil {
		log.Fatalln("Failed getting object as anonymous user", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Bad status %q", resp.Status)
	}

	file := "s3-local-file-anonymous.jpg"
	os.Remove(file)
	localFile, err := os.Create(file)
	if err != nil {
		return err
	}

	_, err = io.Copy(localFile, resp.Body)

	return err
}

func main() {
	// Setup radosgw users.
	removeUser("lxdadmin")
	removeUser("testread")
	removeUser("testwrite")

	adminKey, err := addUser("lxdadmin", 0)
	if err != nil {
		log.Fatalln("Failed creating lxdadmin user", err)
	}

	adminUser := client(adminKey.AccessKey, adminKey.SecretKey)

	fmt.Printf("Created lxdadmin user: %+v\n", adminKey)

	testUserReadKey, err := addUser("testread", -1)
	if err != nil {
		log.Fatalln("Failed creating testread user", err)
	}

	testUserRead := client(testUserReadKey.AccessKey, testUserReadKey.SecretKey)

	fmt.Printf("Created testread user: %+v\n", adminKey)

	testUserWriteKey, err := addUser("testwrite", -1)
	if err != nil {
		log.Fatalln("Failed creating testwrite user", err)
	}

	testUserWrite := client(testUserWriteKey.AccessKey, testUserWriteKey.SecretKey)

	fmt.Printf("Created testwrite user: %+v\n", adminKey)

	// Clean up state.
	for _, bucketName := range []string{"mybucket", "mybucket2"} {
		removeObject(adminUser, bucketName)
		bucketExists, err := adminUser.BucketExists(context.Background(), bucketName)
		if err != nil {
			log.Fatalln("Failed checking mybucket exists", err)
		}

		if bucketExists {
			err = adminUser.RemoveBucket(context.Background(), bucketName)
			if err != nil {
				log.Fatalf("Failed removing %s: %v\n", bucketName, err)
			}

			fmt.Printf("Removed %s\n", bucketName)
		}
	}

	// Check cannot create bucket as testwrite user.
	err = testUserWrite.MakeBucket(context.Background(), "mybucket", minio.MakeBucketOptions{})
	if err == nil {
		log.Fatalln("testwrite shouldn't be able to create buckets")
	}

	// Create a mybucket owned by admin user.
	err = adminUser.MakeBucket(context.Background(), "mybucket", minio.MakeBucketOptions{})
	if err != nil {
		log.Fatalln("Failed creating mybucket owned by admin user", err)
	}

	// Create a mybucket2 owned by admin user.
	err = adminUser.MakeBucket(context.Background(), "mybucket2", minio.MakeBucketOptions{})
	if err != nil {
		log.Fatalln("Failed creating mybucket2 owned by admin user", err)
	}

	fmt.Println("Created buckets as admin user")

	// Set bucket policy without anonymous access.
	err = setBucketPolicy(adminUser, false, false)
	if err != nil {
		log.Fatalln("Failed setting mybucket policy by admin user", err)
	}

	fmt.Println("Admin set mybucket policy without anonymous access")

	// Put object as admin user into buckets.
	err = putObject(adminUser, "mybucket")
	if err != nil {
		log.Fatalln("Failed putting object into mybucket as admin user", err)
	}

	err = putObject(adminUser, "mybucket2")
	if err != nil {
		log.Fatalln("Failed putting object into mybucket2 as admin user", err)
	}

	// Get object upload by admin as testwrite user.
	err = getObject(testUserWrite, "mybucket")
	if err != nil {
		log.Fatalln("Failed getting object as testwrite user", err)
	}

	// Get object upload by admin as testwrite user.
	err = getObject(testUserWrite, "mybucket2")
	if err == nil {
		log.Fatalln("Shouldn't be able to get object from mybucket2 as testwrite user")
	}

	// Get object uploaded by admin as testread user.
	err = getObject(testUserRead, "mybucket2")
	if err == nil {
		log.Fatalln("Shouldn't be able to get object from mybucket2 as testread user")
	}

	// Test anonymous access.
	err = getObjectAnonymous("mybucket2")
	if err == nil {
		log.Fatalln("Anonymous user shouldn't be able to get object from mybucket2")
	}

	// Put object as testwrite user.
	err = putObject(testUserWrite, "mybucket2")
	if err == nil {
		log.Fatalln("Shouldn't be able to put object into mybucket2 as testwrite user")
	}

	// Put object as testwrite user.
	err = putObject(testUserRead, "mybucket2")
	if err == nil {
		log.Fatalln("Shouldn't be able to put object into mybucket2 as testread user")
	}

	// Put object as testwrite user.
	err = putObject(testUserWrite, "mybucket")
	if err != nil {
		log.Fatalln("Failed putting object as testwrite user", err)
	}

	fmt.Println("Put myobject as testwrite user")

	// Get object as testwrite user.
	err = getObject(testUserWrite, "mybucket")
	if err != nil {
		log.Fatalln("Failed getting object as testwrite user", err)
	}

	fmt.Println("Got myobject as testwrite user")

	// Get object as testread user.
	err = getObject(testUserRead, "mybucket")
	if err == nil {
		log.Fatalln("testread user shouldn't be able to get myobject")
	}

	// Test anonymous access.
	err = getObjectAnonymous("mybucket")
	if err == nil {
		log.Fatalln("Anonymous user shouldn't be able to get myobject")
	}

	// Set bucket policy without testread user access.
	err = setBucketPolicy(adminUser, true, false)
	if err != nil {
		log.Fatalln("Failed setting mybucket policy by admin user", err)
	}

	fmt.Println("Admin set mybucket policy with testread access")

	// Get object as testread user.
	err = getObject(testUserRead, "mybucket")
	if err != nil {
		log.Fatalln("Failed getting object as testread user", err)
	}

	fmt.Println("Got myobject as testread user")

	// Test anonymous access.
	err = getObjectAnonymous("mybucket")
	if err == nil {
		log.Fatalln("Anonymous user shouldn't be able to get myobject")
	}

	// Set bucket policy with anonymous access.
	err = setBucketPolicy(adminUser, false, true)
	if err != nil {
		log.Fatalln("Failed setting mybucket policy by admin user", err)
	}

	fmt.Println("Admin set mybucket policy with anonymous access")

	// Get object as testwrite user.
	err = getObject(testUserWrite, "mybucket")
	if err != nil {
		log.Fatalln("Failed getting object as testwrite user", err)
	}

	fmt.Println("Got myobject as testwrite user")

	// Get object as testread user.
	err = getObject(testUserRead, "mybucket")
	if err != nil {
		log.Fatalln("Failed getting object as testread user", err)
	}

	fmt.Println("Got myobject as testread user")

	// Test anonymous access.
	err = getObjectAnonymous("mybucket")
	if err != nil {
		log.Fatalln("Failed getting object as anonymous user", err)
	}

	fmt.Println("Got myobject as anonymous user")

	// Remove object as testread user.
	err = removeObject(testUserRead, "mybucket")
	if err == nil {
		log.Fatalln("Shouldn't be able to remove myobject as testread user")
	}

	// Remove object as testwrite user.
	err = removeObject(testUserWrite, "mybucket")
	if err != nil {
		log.Fatalln("Failed removing object as testwrite user", err)
	}

	fmt.Println("Removed myobject as testwrite user")
}
