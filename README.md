# Cadence [![Build Status](https://badge.buildkite.com/159887afd42000f11126f85237317d4090de97b26c287ebc40.svg?theme=github&branch=master)](https://buildkite.com/uberopensource/cadence-server) [![Coverage Status](https://coveralls.io/repos/github/uber/cadence/badge.svg?branch=master)](https://coveralls.io/github/uber/cadence?branch=master)

Cadence is a distributed, scalable, durable, and highly available orchestration engine we developed at Uber Engineering to execute asynchronous long-running business logic in a scalable and resilient way.

Business logic is modeled as workflows and activities. Workflows are the implementation of coordination logic. Its sole purpose is to orchestrate activity executions. Activities are the implementation of a particular task in the business logic. The workflow and activity implementation are hosted and executed in worker processes. These workers long-poll the Cadence server for tasks, execute the tasks by invoking either a workflow or activity implementation, and return the results of the task back to the Cadence server. Furthermore, the workers can be implemented as completely stateless services which in turn allows for unlimited horizontal scaling.

The Cadence server brokers and persists tasks and events generated during workflow execution, which provides certain scalability and reliability guarantees for workflow executions. An individual activity execution is not fault tolerant as it can fail for various reasons. But the workflow that defines in which order and how (location, input parameters, timeouts, etc.) activities are executed is guaranteed to continue execution under various failure conditions.

This repo contains the source code of the Cadence server. To implement workflows, activities and worker use [Go client](https://github.com/uber-go/cadence-client) or [Java client](https://github.com/uber-java/cadence-client).

See Maxim's talk at [Data@Scale Conference](https://atscaleconference.com/videos/cadence-microservice-architecture-beyond-requestreply) for an architectural overview of Cadence.

## Getting Started

### Start the cadence-server locally

* Build the required binaries following the instructions [here](CONTRIBUTING.md).

* Install and run `cassandra` locally:

```bash
# for OS X
brew install cassandra

# start cassandra
/usr/local/bin/cassandra
```

* Setup the cassandra schema:

```bash
make install-schema
```

* Start the service:

```bash
./cadence-server start
```

### Using Docker

You can also [build and run](docker/README.md) the service using Docker.

### Run the Samples

Try out the sample recipes [here](https://github.com/samarabbas/cadence-samples) to get started.

### Use CLI

Try out [Cadence command-line tool](tools/cli/README.md) to perform various tasks on Cadence

### Use Cadence Web

Try out [Cadence Web UI](https://github.com/uber/cadence-web) to view your workflows on Cadence.  
(This is already available at localhost:8088 if you run Cadence with docker compose)

## Contributing

We'd love your help in making Cadence great. Please review our [contribution guide](CONTRIBUTING.md).

If you'd like to propose a new feature, first join the Cadence [discussion group](https://groups.google.com/d/forum/cadence-discussion) and [Slack channel](https://join.slack.com/t/uber-cadence/shared_invite/enQtNDczNTgxMjYxNDEzLTI5Yzc5ODYwMjg1ZmI3NmRmMTU1MjQ0YzQyZDc5NzMwMmM0NjkzNDE5MmM0NzU5YTlhMmI4NzIzMDhiNzFjMDM) to start a discussion and check if there are existing design discussions. Also peruse our [design docs](docs/design/index.md) in case a feature has been designed but not yet implemented. Once you're sure the proposal is not covered elsewhere, please follow our [proposal instructions](PROPOSALS.md).

## License

MIT License, please see [LICENSE](https://github.com/uber/cadence/blob/master/LICENSE) for details.
