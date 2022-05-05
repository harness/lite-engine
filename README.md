# Lite-engine

How to use:

* Create an .env file. It can be empty.
* To build linux and windows `GOOS=windows go build -o lite-engine.exe; go build`
* Generate tls credentials: go run main.go certs
* Start server: go run main.go server
* Client call to check health status of server: go run main.go client.

## Release procedure

Run the changelog generator.

```BASH
docker run -it --rm -v "$(pwd)":/usr/local/src/your-app githubchangeloggenerator/github-changelog-generator -u harness -p lite-engine -t <secret github token>
```

You can generate a token by logging into your GitHub account and going to Settings -> Personal access tokens.

Next we tag the PR's with the fixes or enhancements labels. If the PR does not fulfil the requirements, do not add a label.

**Before moving on make sure to update the version file `version/version.go`.**

Run the changelog generator again with the future version according to semver.

```BASH
docker run -it --rm -v "$(pwd)":/usr/local/src/your-app githubchangeloggenerator/github-changelog-generator -u harness -p lite-engine -t <secret token> --future-release v0.2.0
```

Create your pull request for the release. Get it merged then tag the release.
