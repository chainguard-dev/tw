import os, sys, traceback, unittest
import upstream_tests_pytest as ut  # network guard, destdir pin, test-module finders

class Result(unittest.TextTestResult):
    def _why(self, err):
        try:
            return " ".join(traceback.format_exception_only(err[0], err[1])[-1].split())[:300]
        except Exception:
            return ""
    def addSuccess(self, test):
        super().addSuccess(test)
        ut._set(test.id(), "passed")
    def addFailure(self, test, err):
        super().addFailure(test, err)
        ut._set(test.id(), "failed")
        ut.why_failed.setdefault(test.id(), self._why(err))
    def addError(self, test, err):
        super().addError(test, err)
        name = type(test).__name__
        if name in ("_FailedTest", "ModuleImportFailure"):
            # a module that failed to import: a collection error
            ut.collect_errors.append((test.id().replace("unittest.loader._FailedTest.", ""), "".join(traceback.format_exception(*err))[-4000:]))
        else:
            ut._set(test.id(), "error")
            ut.why_failed.setdefault(test.id(), self._why(err))
    def addSkip(self, test, reason):
        super().addSkip(test, reason)
        ut._set(test.id(), "skipped")
        ut.skips[reason] = ut.skips.get(reason, 0) + 1
    def addExpectedFailure(self, test, err):
        super().addExpectedFailure(test, err)
        ut._set(test.id(), "xfailed")
    def addUnexpectedSuccess(self, test):
        super().addUnexpectedSuccess(test)  # strict, like pytest's xfail_strict
        ut._set(test.id(), "failed")
        ut.why_failed.setdefault(test.id(), "unexpected success")
    def addSubTest(self, test, subtest, err):
        super().addSubTest(test, subtest, err)
        if err is not None:
            ut._set(test.id(), "failed")
            ut.why_failed.setdefault(test.id(), self._why(err))

if __name__ == "__main__":
    ut._guard()
    ut._front()
    import signal
    signal.signal(signal.SIGTERM, ut._on_term)
    prog = unittest.main(module=None, argv=["python -m unittest"] + sys.argv[1:], exit=False,
                         testRunner=unittest.TextTestRunner(resultclass=Result, verbosity=1, stream=sys.stdout))
    res = prog.result
    rc = 0 if res.wasSuccessful() else 1
    if res.testsRun == 0:
        rc = 5
    ut._dump_mods("main")
    ut._dump(rc, res.testsRun)
    sys.exit(rc)
