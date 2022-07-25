/*
This program is for experimenting with Ceph RadosGW access controls.
It performs the following steps:
1. Creates a lxdadmin user that can create buckets.
2. Creates a testuser user that cannot create buckets, with a read subuser.
3. Creates a bucket as lxdadmin user and then changes ownership of it to testuser.
4. Performs various operations to check that testuser and testuser:read cannot do actions they shouldn't.
*/

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
	"strings"

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
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf(string(exiterr.Stderr))
		}

		return err
	}

	return nil
}

func addUser(user string, buckets int) (*Key, error) {
	cmd := exec.Command("radosgw-admin", "user", "create", fmt.Sprintf("--max-buckets=%d", buckets), fmt.Sprintf("--display-name=%s", user), fmt.Sprintf("--uid=%s", user))
	buf, err := cmd.Output()
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf(string(exiterr.Stderr))
		}

		return nil, err
	}

	info := struct {
		Keys []Key `json:"keys"`
	}{}

	err = json.Unmarshal(buf, &info)
	if err != nil {
		return nil, err
	}

	for _, key := range info.Keys {
		if key.User == user {
			return &key, err
		}
	}

	return nil, fmt.Errorf("Key not found")
}

func addSubUser(user string, subuser string, access string) (*Key, error) {
	cmd := exec.Command("radosgw-admin", "subuser", "create", "--gen-access-key", "--key-type=s3", fmt.Sprintf("--uid=%s", user), fmt.Sprintf("--subuser=%s", subuser), fmt.Sprintf("--access=%s", access))
	buf, err := cmd.Output()
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf(string(exiterr.Stderr))
		}

		return nil, err
	}

	info := struct {
		Keys []Key `json:"keys"`
	}{}

	err = json.Unmarshal(buf, &info)
	if err != nil {
		return nil, err
	}

	for _, key := range info.Keys {
		if key.User == fmt.Sprintf("%s:%s", user, subuser) {
			return &key, err
		}
	}

	return nil, fmt.Errorf("Key not found")
}

func bucketLink(bucket string, user string) error {
	cmd := exec.Command("radosgw-admin", "bucket", "link", fmt.Sprintf("--bucket=%s", bucket), fmt.Sprintf("--uid=%s", user))
	_, err := cmd.Output()
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf(string(exiterr.Stderr))
		}

		return err
	}

	return nil
}

func bucketQuota(user string, size string) error {
	cmd := exec.Command("radosgw-admin", "quota", "set", "--quota-scope=bucket", fmt.Sprintf("--uid=%s", user), fmt.Sprintf("--max-size=%s", size))
	_, err := cmd.Output()
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf(string(exiterr.Stderr))
		}

		return err
	}

	cmd = exec.Command("radosgw-admin", "quota", "enable", "--quota-scope=bucket", fmt.Sprintf("--uid=%s", user))
	_, err = cmd.Output()
	if err != nil {
		if exiterr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf(string(exiterr.Stderr))
		}

		return err
	}

	return nil
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

func setBucketPolicy(client *minio.Client) error {
	// The default bucket policy just allows the testwrite user to GET/PUT/DELETE objects in bucket.
	policy := `{
		"Version": "2012-10-17",
		"Statement": [{
			"Effect": "Allow",
			"Principal": "*",
			"Action": [
				"s3:GetObject",
				"s3:GetObjectVersion"
			],
			"Resource": [
				"arn:aws:s3:::mybucket/*"
			]
		}]
	}`

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

func removeBucket(client *minio.Client, bucket string) error {
	return client.RemoveBucket(context.Background(), bucket)
}

func main() {
	// Setup radosgw users.
	removeUser("lxdadmin")
	removeUser("testread")
	removeUser("testwrite2")

	// Create LXD admin user which is allowed to create buckets.
	adminKey, err := addUser("lxdadmin", 0)
	if err != nil {
		log.Fatalln("Failed creating lxdadmin user", err)
	}

	adminUser := client(adminKey.AccessKey, adminKey.SecretKey)

	fmt.Printf("Created lxdadmin user: %+v\n", adminKey)

	_, err = addUser("testwrite2", -1)
	if err != nil {
		log.Fatalln("Failed creating testwrite user", err)
	}

	testUserWriteKey, err := addSubUser("testwrite2", "write", "full")
	if err != nil {
		log.Fatalln("Failed creating testread user", err)
	}

	testUserWrite := client(testUserWriteKey.AccessKey, testUserWriteKey.SecretKey)

	fmt.Printf("Created testwrite user: %+v\n", testUserWriteKey)

	testUserReadKey, err := addSubUser("testwrite2", "read", "read")
	if err != nil {
		log.Fatalln("Failed creating testread user", err)
	}

	testUserRead := client(testUserReadKey.AccessKey, testUserReadKey.SecretKey)

	fmt.Printf("Created testread user: %+v\n", testUserReadKey)

	// Check cannot create bucket as testwrite user.
	err = testUserWrite.MakeBucket(context.Background(), "mybucket", minio.MakeBucketOptions{})
	if err == nil {
		log.Fatalln("testwrite shouldn't be able to create buckets")
	}

	// Check cannot create bucket as testread user.
	err = testUserRead.MakeBucket(context.Background(), "mybucket", minio.MakeBucketOptions{})
	if err == nil {
		log.Fatalln("testread shouldn't be able to create buckets")
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

	// Change ownership of mybucket to testwrite.
	err = bucketLink("mybucket", "testwrite2")
	if err != nil {
		log.Fatalln("Failed changing ownership of mybucket to testwrite user", err)
	}

	buckets, err := testUserWrite.ListBuckets(context.Background())
	if err != nil {
		log.Fatalln("Failed listing buckets as testwrite user", err)
	}
	if len(buckets) != 1 || buckets[0].Name != "mybucket" {
		log.Fatal("Unexpected buckets in list", buckets)
	}

	fmt.Println("Changed ownership of mybucket to testwrite user")

	err = bucketQuota("testwrite2", "1M")
	if err != nil {
		log.Fatalln("Failed setting bucket quota for testwrite user", err)
	}

	fmt.Println("Set bucket quota for testwrite user to 1MiB")

	// Check cannot create bucket as testwrite user after quota enabled.
	err = testUserWrite.MakeBucket(context.Background(), "mybucket3", minio.MakeBucketOptions{})
	if err == nil {
		log.Fatalln("testwrite shouldn't be able to create buckets")
	}

	// Check cannot put object as testwrite user into bucket owned by testwrite user if would exceed quota.
	err = putObject(testUserWrite, "mybucket")
	if err == nil || !strings.Contains(err.Error(), "QuotaExceeded") {
		log.Fatalln("Shouldn't be able to put object as quota should be exceeded")
	}

	err = bucketQuota("testwrite2", "20M")
	if err != nil {
		log.Fatalln("Failed setting bucket quota for testwrite user", err)
	}

	fmt.Println("Set bucket quota for testwrite user to 20MiB")

	// Put object as testwrite user into bucket owned by testwrite user.
	err = putObject(testUserWrite, "mybucket")
	if err != nil {
		log.Fatalln("Failed putting object into mybucket as testwrite user", err)
	}

	fmt.Println("Put myobject as testwrite user")

	// Check lxdadmin user cannot put object into mybucket which is owned by testuser.
	err = putObject(adminUser, "mybucket")
	if err == nil {
		log.Fatalln("Shouldn't be able to put object into mybucket as lxdadmin user")
	}

	// Check lxdadmin user can put object into mybucket2 which is owned by lxdadmin.
	err = putObject(adminUser, "mybucket2")
	if err != nil {
		log.Fatalln("Failed putting object into mybucket2 as lxdadmin user", err)
	}

	fmt.Println("Put myobject2 as lxdadmin user")

	// Get object uploaded by testwrite into bucket owned by testwrite as testwrite user.
	err = getObject(testUserWrite, "mybucket")
	if err != nil {
		log.Fatalln("Failed getting object as testwrite user", err)
	}

	// Check can't get object in mybucket2 owned by lxdadmin user as testwrite user.
	err = getObject(testUserWrite, "mybucket2")
	if err == nil {
		log.Fatalln("Shouldn't be able to get object from mybucket2 as testwrite user")
	}

	// Check can't get object in mybucket2 owned by lxdadmin user as testread user.
	err = getObject(testUserRead, "mybucket2")
	if err == nil {
		log.Fatalln("Shouldn't be able to get object from mybucket2 as testread user")
	}

	// Check can't get object in mybucket2 owned by lxdadmin user as anonymous user.
	err = getObjectAnonymous("mybucket2")
	if err == nil {
		log.Fatalln("Anonymous user shouldn't be able to get object from mybucket2")
	}

	// Check can't put object in mybucket2 owned by lxdadmin user as testwrite user.
	err = putObject(testUserWrite, "mybucket2")
	if err == nil {
		log.Fatalln("Shouldn't be able to put object into mybucket2 as testwrite user")
	}

	// Check can't put object in mybucket2 owned by lxdadmin user as testread user.
	err = putObject(testUserRead, "mybucket2")
	if err == nil {
		log.Fatalln("Shouldn't be able to put object into mybucket2 as testread user")
	}

	// Put object as testwrite user into bucket owned by testwrite user.
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
	// Although this appears to be a different user, in ceph radosgw world, it is owned by the same underlying
	// user as the testwrite user, and it appears that you can always get the objects you put.
	err = getObject(testUserRead, "mybucket")
	if err != nil {
		log.Fatalln("Failed getting object as testread user", err)
	}

	fmt.Println("Got myobject as testread user")

	// Check can't get object in mybucket owned by testwrite user as anonymous user.
	err = getObjectAnonymous("mybucket")
	if err == nil {
		log.Fatalln("Anonymous user shouldn't be able to get myobject")
	}

	// Check testread user cannot set bucket policy (even on buckets owned by same underlying user).
	err = setBucketPolicy(testUserRead)
	if err == nil {
		log.Fatalln("Shouldn't be able to set bucket policy as testread user", err)
	}

	// Set bucket policy to allow anonymous get access.
	err = setBucketPolicy(testUserWrite)
	if err != nil {
		log.Fatalln("Failed setting mybucket anonymous access policy by testwrite user", err)
	}

	fmt.Println("Set mybucket policy with anonymous access by testwrite user")

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

	// Check can't remove object from mybucket as testread user even though owned by the same underlying user.
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

	// Check testread user cannot remove mybucket.
	err = removeBucket(testUserRead, "mybucket")
	if err == nil {
		log.Fatalln("Shouldn't be able to remove mybucket as testread user")
	}

	// Check testwrite user can remove mybucket.
	err = removeBucket(testUserWrite, "mybucket")
	if err != nil {
		log.Fatalln("Failed removing object as testwrite user", err)
	}

	fmt.Println("Removed mybucket as testwrite user")

	// Check testwrite user cannot remove mybucket2.
	err = removeBucket(testUserWrite, "mybucket2")
	if err == nil {
		log.Fatalln("Shouldn't be able to remove mybucket2 as testwrite user")
	}
}
