default: tailr

tailr:  *.go
	go build

clean:
	rm -rf tailr
