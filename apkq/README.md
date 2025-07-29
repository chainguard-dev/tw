# apkq - apk query
An 'apk' program that works like 'apk info' for basic
use cases like checking packages installed and listing package
files.

This allows tools like 'ldd-check' and others to use it instead
of 'apk-tools' and not pollute the test environment with apk.
