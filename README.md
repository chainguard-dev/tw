# tw (tee-dub)
tw (pronounced tee-dub) is a centralized repository for testing and building
tools or helpers.

## Release to stereo
To release a version of tw to stereo, run tools/release-to-stereo.

    $ git tag vX.Y.Z
    $ git push origin vX.Y.Z
    $ ./tools/release-to-stereo vX.Y.Z ~/git/cg/chainguard-dev/stereo/

This takes care of updating the `tw.yaml` file from `melange.yaml`,
and syncs the pipeline files for other dirs.

That will do a commit and you just need to push and do a PR.

## Testing locally

In order to test a tw pipeline in a local stereo repository, you need the following steps:

* Build the tw tools package in this repository, `make build`.
* If required, sync the pipeline yaml to stereo by hand.
* If required, build the melange package with the new pipeline, in the stereo repository.
* If required, test the melange package with the new pipeline, in the stereo repository.

Most likely, you need to tell the melange build in the stereo repository to use the tw index.

A complete example:

```
user@debian:~git/tw $ make build
user@debian:~git/tw $ cp pipelines/test/tw/something.yaml ~/git/stereo/os/pipelines/test/tw/
user@debian:~git/tw $ cd ~/git/stereo/enterprise-packages/
user@debian:~git/stereo/os $ make debug/somepackage
user@debian:~git/stereo/os $ MELANGE_DEBUG_TEST_OPTS="--ignore-signatures --repository-append ~/git/tw/packages" make test-debug/somepackage
```
