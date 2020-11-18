package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"github.com/dustin/go-humanize"

	flags "github.com/jessevdk/go-flags"
)

//VERSION -
var version string

var (
	//ErrFileNotFound -
	ErrCredentialsNotFound = errors.New("Credentials not found")
)

type options struct {
	Dir          string `long:"dir" description:"Output directory"`
	LogGroupName string `long:"group" description:"Group name" default:"/aws/batch/job"`
	Prefix       string `long:"prefix" description:"Filters the results" required:"true"`
	Proxy        string `long:"proxy" description:"Proxy"`
	From         string `long:"from" description:"created time filter from"`
	To           string `long:"to" description:"create time filter to "`
	List         bool   `long:"list" description:"list file"`
	Process      int    `short:"p" long:"process" descritpion:"download process"`
}

//AwsCredentials -
type AwsCredentials struct {
	AccessKeyID     string `json:"aws_access_key_id"`
	SecretAccessKey string `json:"aws_secret_access_key"`
	Region          string `json:"region"`
}

//LLogStream - Local LogStream
type LLogStream struct {
	LogGroupName       string
	LogStreamName      string
	FileName           string
	LastEventTimestamp int64
	CreationTime       int64
	StoredBytes        int64
}

func saveLogStream(log *LLogStream, f *os.File, svc *cloudwatchlogs.CloudWatchLogs, token *string) *string {

	params := &cloudwatchlogs.GetLogEventsInput{
		LogGroupName:  aws.String(log.LogGroupName),
		LogStreamName: aws.String(log.LogStreamName),
		StartFromHead: aws.Bool(true),
		NextToken:     token,
	}

	logresp, err := svc.GetLogEvents(params)
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}

	if token != nil && *token == *logresp.NextForwardToken {
		return nil
	}

	gotToken := ""
	nextToken := ""

	for _, event := range logresp.Events {
		gotToken = nextToken
		nextToken := *logresp.NextForwardToken

		if gotToken == nextToken {
			break
		}

		if _, err = f.WriteString(*event.Message + "\r\n"); err != nil {
			break
		}
	}

	return logresp.NextForwardToken
}

func listLogStreams(out chan *LLogStream, svc *cloudwatchlogs.CloudWatchLogs, opts *options, from int64, to int64, token *string) *string {
	params := &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName:        aws.String(opts.LogGroupName), // Required
		LogStreamNamePrefix: aws.String(opts.Prefix),
		Descending:          aws.Bool(true),
		NextToken:           token,
		// OrderBy:             aws.String("LastEventTime"),
	}

	resp, err := svc.DescribeLogStreams(params)
	if err != nil {
		fmt.Println(err)
		return nil
	}

	for _, log := range resp.LogStreams {
		fmt.Println(log.GoString())
		if from > 0 && *log.CreationTime < from {
			continue
		}
		if to > 0 && *log.CreationTime > to {
			continue
		}

		//Time
		creationTime := time.Unix(0, *log.CreationTime*int64(time.Millisecond)).Format("2006-01-02 15:04:05.000")
		//Filename
		fname := *log.LogStreamName
		fname = strings.ReplaceAll(fname, "/", "_")
		if opts.List {
			fmt.Printf("%+v %+v\n", fname, creationTime)
		} else {
			fmt.Printf("\rget %+v %+v", fname, creationTime)
		}

		fname = filepath.Join(opts.Dir, fname)
		if !opts.List {
			os.Remove(fname)
		}

		f := &LLogStream{
			FileName:      fname,
			LogGroupName:  opts.LogGroupName,
			LogStreamName: *log.LogStreamName,
			// LastEventTimestamp: *log.LastEventTimestamp,
			CreationTime: *log.CreationTime,
			StoredBytes:  *log.StoredBytes,
		}

		out <- f

	}

	return resp.NextToken
}

func main() {
	var opts options
	if _, err := flags.Parse(&opts); err != nil {
		os.Exit(-1)
	}

	fmt.Println("AWS CloudWatch log ", version)
	fmt.Println("Created by ditto.")

	if len(opts.LogGroupName) == 0 {
		fmt.Println("Please specify a Log Group Name with --group")
		os.Exit(-1)
	}

	if !opts.List && len(opts.Dir) == 0 {
		fmt.Println("Please specify a output directory with --dir")
		os.Exit(-1)
	}

	process := opts.Process
	if process == 0 {
		process = 3
	}
	ftime := parseTime(opts.From)
	ttime := parseTime(opts.To)
	if ftime == -1 || ttime == -1 {
		fmt.Println("Invalid date format.")
		os.Exit(-1)
	}

	if ttime > 0 && ftime > ttime {
		fmt.Println("From time can't greater than to time.")
		os.Exit(-1)
	}

	ac, err := getAccessCode(opts.Proxy)
	if err != nil {
		fmt.Println("No valid credential information in the environment.")
		os.Exit(-1)
	}

	if !opts.List {
		if err := os.MkdirAll(opts.Dir, 0655); err != nil {
			fmt.Println(err)
			os.Exit(-1)
		}
	}

	if len(ac.Region) == 0 {
		ac.Region = "ap-northeast-1"
	}
	sess := getSession(ac, opts.Proxy)

	svc := cloudwatchlogs.New(sess)

	var totalSize int64
	fileCount := 0

	keysChan := make(chan *LLogStream, 1000)

	wg := new(sync.WaitGroup)
	for i := 0; i < process; i++ {
		wg.Add(1)

		go func(wg *sync.WaitGroup) {
			defer wg.Done()

			for fi := range keysChan {
				fileCount++
				totalSize += fi.StoredBytes

				if opts.List {
					continue
				}

				//Output
				f, err := os.OpenFile(fi.FileName, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				if err != nil {
					fmt.Println(err.Error())
					return
				}
				defer f.Close()

				logtoken := saveLogStream(fi, f, svc, nil)
				for logtoken != nil {
					logtoken = saveLogStream(fi, f, svc, logtoken)
				}

				f.Close()
				//Change time stamp
				err = os.Chtimes(fi.FileName,
					time.Unix(0, fi.LastEventTimestamp*int64(time.Millisecond)),
					time.Unix(0, fi.CreationTime*int64(time.Millisecond)))
				if err != nil {
					fmt.Println(err.Error())
				}

			}
		}(wg)
	}

	//Get all files list
	token := listLogStreams(keysChan, svc, &opts, ftime, ttime, nil)
	for token != nil {
		token = listLogStreams(keysChan, svc, &opts, ftime, ttime, token)
	}

	close(keysChan)
	wg.Wait()

	//Done
	fmt.Println()
	fmt.Printf("FileCount: %+v, TotalSize: %+v (%+v)\n", fileCount, humanize.Bytes(uint64(totalSize)), humanize.Comma(totalSize))
	fmt.Println("Finished.")
}

func getAccessCode(proxy string) (*AwsCredentials, error) {
	ac := AwsCredentials{}

	homedir, err := dirWindows()
	if err != nil {
		fmt.Println("Failed to get current user")
	} else {
		homedir = filepath.ToSlash(homedir)
	}

	credsfile := fmt.Sprintf("%s/.aws/credentials", homedir)
	creds := credentials.NewSharedCredentials(credsfile, "default")
	credValue, err := creds.Get()
	if err != nil {
		//環境変数
		ac.AccessKeyID = os.Getenv("AWS_ACCESS_KEY")
		ac.SecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
		ac.Region = os.Getenv("AWS_DEFAULT_REGION")
	} else {
		ac.AccessKeyID = credValue.AccessKeyID
		ac.SecretAccessKey = credValue.SecretAccessKey
	}

	//Credentials not found
	if len(ac.AccessKeyID) == 0 || len(ac.SecretAccessKey) == 0 {
		return nil, ErrCredentialsNotFound
	}

	return &ac, nil
}

func getSession(ac *AwsCredentials, proxy string) *session.Session {
	// var timeout time.Duration
	// timeout = time.Duration(30) * time.Second
	//Proxy
	var httpClient *http.Client
	if len(proxy) > 0 {
		httpClient = &http.Client{
			Transport: &http.Transport{
				Proxy: func(*http.Request) (*url.URL, error) {
					return url.Parse(proxy)
				},
			},
		}
	}

	//認証情報を作成します。
	cred := credentials.NewStaticCredentials(
		ac.AccessKeyID,
		ac.SecretAccessKey,
		"")

	//セッション作成します
	sess := session.Must(session.NewSession(&aws.Config{
		Region:      aws.String(ac.Region),
		Credentials: cred,
		HTTPClient:  httpClient,
	}))

	return sess
}

func dirWindows() (string, error) {
	// First prefer the HOME environmental variable
	if home := os.Getenv("HOME"); home != "" {
		return home, nil
	}

	// Prefer standard environment variable USERPROFILE
	if home := os.Getenv("USERPROFILE"); home != "" {
		return home, nil
	}

	drive := os.Getenv("HOMEDRIVE")
	path := os.Getenv("HOMEPATH")
	home := drive + path
	if drive == "" || path == "" {
		return "", errors.New("HOMEDRIVE, HOMEPATH, or USERPROFILE are blank")
	}

	return home, nil
}

//parseTime -
func parseTime(tm string) int64 {
	var date int64
	if len(tm) > 0 {
		if len(tm) < 4 {
			return -1
		}

		if len(tm) == 4 {
			tm = tm + "01"
		}
		if len(tm) == 6 {
			tm = tm + "01"
		}
		if len(tm) < 14 {
			tm = tm + fmt.Sprintf("%0*d", 14-len(tm), 0)
		}

		t, err := time.ParseInLocation(`20060102150405`, tm, time.Now().Location())
		//t, err := time.Parse("20060102150405", tm)
		if err == nil {
			date = t.UnixNano() / 1e6
		}
	}
	return date
}
