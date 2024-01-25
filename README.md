# dynamo

The **dynamo** service is responsible for generating images and other resources based on
user-submitted prompts. These generated resources are typically used for alerts that
appear on-screen during streams.

In constrast to typical static alerts, these types of alerts:

- Require generating dynamic content before the alert can be displayed, which is
  typically a long-running background process which may fail or require retries
- Depend on external services which may be unavailable or subject to rate limiting,
  requiring robust backoff and retry
- May fail due to impermissible prompts or the detection of objectionable content

The dynamo service is designed to account for these constraints:

- It queues generation requests in order to respect rate limits and handle retries
  automatically and in an orderly fashion
- It handles one of two eventual end states: success, in which case an alert is
  displayed on the stream; or failure, in which case the user is notified that their
  request was inadmissible
- It coordinates with the **ledger** service to ensure that Golden VCR Fun Point costs
  are debited only upon success

The **dynamo-consumer** process consumes events from the
[**generation-requests**][gh-schemas-genreq] RabbitMQ exchange, constructing prompts and
submitting them to the OpenAI API in order to generate the desired assets. Once assets
are successfully generated, the requisite Golden VCR Fun Points are deducted from the
user's balance, and an alert is initiated by producing a message to the
[**onscreen-events**][gh-schemas-eonscreen] exchange.

The **dynamo** server process allows HTTP clients to obtain information about existing
generation requests and to requests to the queue manually, outside of the Twitch event
pipeline. State pertaining to asset generation requests is stored in a PostgreSQL
database schema called `dynamo`.

[gh-schemas-genreq]: https://github.com/golden-vcr/schemas?tab=readme-ov-file#generation-requests
[gh-schemas-eonscreen]: https://github.com/golden-vcr/schemas?tab=readme-ov-file#onscreen-events

## Prerequisites

Install [Go 1.21](https://go.dev/doc/install). If successful, you should be able to run:

```
> go version
go version go1.21.0 windows/amd64
```

## Initial setup

Create a file in the root of this repo called `.env` that contains the environment
variables required in [`main.go`](./cmd/consumer/main.go). If you have the
[`terraform`](https://github.com/golden-vcr/terraform) repo cloned alongside this one,
simply open a shell there and run:

- `terraform output -raw auth_shared_secret_env >> ../dynamo/.env`
- `terraform output -raw openai_api_env >> ../dynamo/.env`
- `terraform output -raw user_images_s3_env >> ../dynamo/.env`
- `./local-rmq.sh env >> ../dynamo/.env`
- `./local-db.sh env >> ../dynamo/.env`

The dynamo service depends on both RabbitMQ and PostgreSQL: you can start up local
versions for development (in a docker container) by running `./local-rmq.sh up` and
`./local-db.sh up`.
