# tw (tee-dub)
tw (pronounced tee-dub) is a centralized repository for testing and building
tools or helpers.

## Release to Wolfi
To release a version of tw to wolfi, run tools/release-to-wolfi.

    $ git tag vX.Y.Z
    $ git push origin vX.Y.Z
    $ ./tools/release-to-wolfi vX.Y.Z \
       ~/src/wolfi-os/ ~/src/enterprise-packages ~/src/extra-packages

This takes care of updating the `tw.yaml` file from `melange.yaml`
for wolfi, and syncs the pipeline files for other dirs.

That will do a commit and you just need to push and do a PR.

## Testing locally

In order to test a tw pipeline in a local melange repository, you need the following steps:

* Build the tw tools package in this repository, `make build`.
* If required, sync the pipeline yaml to the target melange repository.
* If required, build the melange package with the new pipeline, in the target melange repository.
* If required, test the melange package with the new pipeline, in the target melange repository.

Most likely, you need to tell the melange build in the target local melange repository to use the tw index.

A complete example:

```
user@debian:~git/tw $ make build
user@debian:~git/tw $ cp pipelines/test/tw/something.yaml ~/git/enterprise-packages/pipelines/test/tw/
user@debian:~git/tw $ cd ~/git/enterprise-packages/
user@debian:~git/enterprise-packages $ make debug/somepackage
user@debian:~git/enterprise-packages $ MELANGE_DEBUG_TEST_OPTS="--ignore-signatures --repository-append ~/git/tw/packages" make test-debug/somepackage
```
