# awslogs
AWS CloudWatch log download

This is a simple tool which download aws cloudwatch logs to local.


## How to use
**Usage:**
```
  awslogs.exe [OPTIONS]

Application Options:
      /dir:      Output directory
      /group:    Group name (default: /aws/batch/job)
      /prefix:   Filters the results
      /proxy:    Proxy
      /from:     created time filter from
      /to:       create time filter to
      /list      list file
  /p, /process:

Help Options:
  /?             Show this help message
  /h, /help      Show this help message
  
```

**Between date**<br>
awslogs --dir c:\tmp\logs --prefix batch-bt1200 --from 202008 --to 202009

**List only**<br>
awslogs --dir c:\tmp\logs --prefix batch-bt1200 --from 202008 --to 202009 --list

**Multi-Process download**<br>
awslogs --dir c:\tmp\logs --prefix batch-bt1200 --from 202008 --to 202009 --process 3

## How to build
**Windows**<br>
just execute the batch file **gobuild.cmd**.

**Linux**<br>
execute the batch file **gobuild-linux.cmd**.

