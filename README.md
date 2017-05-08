# Tailr - A Recursive Log Forwarder

`Tailr` watches a folder recursively for files matching a specified pattern. If the file exists it will start reading the file contents and forwards the same in realtime to a remote server. `Tailr` can detect any new files/directories that gets created with in the watching directory. It will automatically start watching all dicrectories and files that gets  created with in the new sub directories. It also automatically stops watching if a file/directory is removed.

`Tailr` also comes with an optional file offset feature where it keeps track of the file offset for each file that it keeps tracks off. If the file offset is enabled and application gets restarted, `Tailr` will start reading the files from where it stopped previously instead of re-reading the file from the begining.

`Tailr` also comes with a ***noop*** output option. If `-noop` flag is passed, `Tailr` will redirect all the forwarded logs to `STDOUT` instead of forwarding to the remote server.

```
$ tailr -h
Usage of tailr:
  -logDir string
    	path to log directory (default "/tmp/logs")
  -logPattern string
    	log pattern to look for (default "*.log")
  -noop
    	prints the output to stdout
  -offsetFilePath string
    	path to offset file store (default "/tmp/tailr-offset.json")
  -port string
    	server port (default "9000")
  -protocol string
    	tcp or udp (default "tcp")
  -server string
    	server address (default "localhost")
  -useOffset
    	whether to use file offset method or not (default true)
```

## Installing

    $ make

## TODO

* Support Bazel build
* Support to more remote output server like Redis/Elastisearch etc ...
* Add some go tests
