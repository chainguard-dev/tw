import collections, glob, json, os, re, sys
E = os.environ
TESTISH = re.compile(r"^(_?tests?|testing|unittests?|conftest|test_.*|.*_tests?)$")
TESTPKG = re.compile(r"^(_?tests?|unittests?|testsuite|test_.*|.*_tests?)$")
def testish(name):
    return any(TESTISH.match(p) for p in name.split(".")[1:])
pkg, me, rc = E["UT_PKG"], E["UT_ME"], int(E["UT_RC"])
out, dsp, src = E["UT_OUT"], E.get("UT_DSP", ""), os.path.realpath(E["UT_SRC"])
why = []
try:
    r = json.load(open(os.path.join(out, "results.json")))
except Exception:
    r = None
if r is None:
    print("UPSTREAM-TESTS %s: pass=unknown fail=unknown skip=unknown" % pkg)
    if rc == 124 or rc >= 128:
        why.append("pytest timed out or was killed (exit %d, overall-timeout=%ss)" % (rc, E["UT_LIMIT"]))
    else:
        why.append("pytest did not start a session (exit %d): bad arguments, an unknown option in addopts, or a conftest import error; see the output above" % rc)
    print("UPSTREAM-TESTS %s: FAILED: %s" % (pkg, "; ".join(why)))
    sys.exit(1)
for n_ in r.get("notes") or []:
    print("%s: note: %s" % (me, n_))
if r.get("stubbed"):
    print("%s: ignored options of plugins that are not installed: %s" % (me, " ".join(r["stubbed"])))
res, cerr = r["results"], r["collect_errors"]
c = collections.Counter(res.values())
fail = c["failed"] + c["error"] + len(cerr)
ign = [t for t in E.get("UT_IGNORE", "").split() if t]
ignored = 0
for t in ign:
    n = sum(1 for d in r["deselected"] if d == t or d.startswith(t + "::") or d.startswith(t + "["))
    ignored += n
    if n:
        print("IGNORE %s: %s (deselected %d)" % (pkg, t, n))
    else:
        print("%s: WARNING: ignore entry '%s' matches no collected test" % (me, t), file=sys.stderr)
if r["skips"]:
    print("%s: most common skip reasons:" % me)
    for reason, n in sorted(r["skips"].items(), key=lambda x: -x[1])[:10]:
        print("  %5d  %s" % (n, reason[:200]))
wf = r.get("why") or {}
if cerr or wf:
    print("%s: failure reasons (first 40):" % me)
    for nodeid, why_ in cerr[:40]:
        lines_ = [l.strip() for l in why_.splitlines() if l.strip()]
        e_ = [l for l in lines_ if l.startswith("E ")]
        print("  %s: %s" % (nodeid, " ".join((e_[-1] if e_ else (lines_[-1] if lines_ else "")).split())[:300]))
    for nodeid in sorted(wf)[:max(0, 40 - len(cerr))]:
        print("  %s: %s" % (nodeid, wf[nodeid]))
for nodeid, why_ in cerr:
    print("UPSTREAM-TESTS-FAIL %s: %s [collection]" % (pkg, nodeid))
for nodeid in sorted(res):
    if res[nodeid] == "failed":
        print("UPSTREAM-TESTS-FAIL %s: %s" % (pkg, nodeid))
    elif res[nodeid] == "error":
        print("UPSTREAM-TESTS-FAIL %s: %s [error]" % (pkg, nodeid))
line = "UPSTREAM-TESTS %s: pass=%d fail=%d skip=%d" % (pkg, c["passed"], fail, c["skipped"])
if c["xfailed"]:
    line += " xfail=%d" % c["xfailed"]
if ignored:
    line += " ignored=%d" % ignored
print(line)
if rc == 124 or rc >= 128:
    why.append("pytest timed out or was killed (exit %d, overall-timeout=%ss)%s" % (rc, E["UT_LIMIT"], " while running %s" % r["killed_during"] if r.get("killed_during") else ""))
if fail:
    why.append("%d failing test(s)/collection error(s)" % fail)
# Under xdist the controller's count can lag; any recorded outcome
# means something was collected.
collected = max(r["collected"], len(res))
if collected == 0 or rc == 5:
    why.append("no tests collected; the suite did not run (check dir, paths, markers, ignore)")
elif c["passed"] == 0:
    why.append("no test passed (everything skipped, xfailed or deselected); the suite did not really run")
else:
    # min-run-percent: the share of the selected tests that actually
    # executed. ran = passed (including non-strict XPASS) + xfailed +
    # failed + errored; selected = ran + skipped, i.e. every test that
    # was collected and not deselected (ignore, markers, -k in args).
    # ignore-files are never collected, so named exceptions do not
    # count against the package.
    ran = c["passed"] + c["xfailed"] + c["failed"] + c["error"]
    sel = ran + c["skipped"]
    if sel and ran * 100 < float(E["UT_MINRUN"]) * sel:
        why.append("only %d of %d collected tests ran (%.1f%%), below min-run-percent=%s; the rest were skipped (reasons above). If that is upstream's default, set min-run-percent lower in the caller with a comment and a TODO(upstream-tests)" % (
            ran, sel, 100.0 * ran / sel, E["UT_MINRUN"]))
if rc not in (0, 1, 5, 124) and rc < 128:
    why.append("pytest exited %d (2 interrupted, 3 internal error, 4 usage error)" % rc)
if rc == 1 and not fail:
    why.append("pytest exited 1 without a recorded failure")
if dsp:
    # Which top-level modules does the package ship, and where were
    # they imported from (pytest process and every xdist worker)?
    own = set()
    for e in os.listdir(dsp) if os.path.isdir(dsp) else []:
        if e.endswith((".dist-info", ".egg-info", ".pth")) or e.startswith(("_distutils_hack", "__pycache__")):
            continue
        own.add(re.split(r"[.]", e)[0])
    # A stray top-level tests/ in the wheel is a packaging bug of its
    # own; the source tree's copy is not "the package under test".
    own -= {"tests", "test", "testing", "conftest", "docs", "benchmarks", "examples"}
    dsp_r = os.path.realpath(dsp)
    hit, shadow = set(), set()
    for f in glob.glob(os.path.join(out, "modules-*.json")):
        for name, path in json.load(open(f)).items():
            top = name.split(".")[0]
            if top not in own:
                continue
            if path.startswith(dsp_r + os.sep):
                hit.add(top)
            elif path.startswith(src + os.sep):
                # Test-only submodules (foo.tests, not in the wheel) come
                # from the source tree by design; anything else is the
                # source tree shadowing (or standing in for) the build.
                rel = os.path.join(dsp, *name.split("."))
                strict = any(TESTPKG.match(p_) for p_ in name.split(".")[1:])
                if not strict and (not testish(name) or os.path.exists(rel + ".py") or os.path.exists(os.path.join(rel, "__init__.py"))):
                    shadow.add("%s (%s)" % (name, path))
    if shadow:
        why.append("the package under test was imported from the source tree, not the destdir: %s; check the project's pytest 'pythonpath' setting and import-mode" % ", ".join(sorted(shadow)))
    elif own and not hit and fail == 0:
        why.append("none of the installed modules (%s) was imported from %s: the suite did not test the build" % (", ".join(sorted(own)), dsp))
# Native extensions: list what the destdir ships, and warn when the
# source tree has extension sources but nothing compiled was installed
# (an optional C/Cython extension that failed to build is otherwise
# silent: setup falls back to pure Python and the suite may still pass).
if dsp and os.path.isdir(dsp):
    sos = []
    for d_, _, fs in os.walk(dsp):
        sos += [os.path.relpath(os.path.join(d_, f_), dsp) for f_ in fs if re.search(r"\.(so|pyd)$", f_)]
    skipd = {"tests", "test", "testing", "docs", "doc", "benchmarks", "bench", "examples", "vendor", "third_party", "thirdparty", "melange-out", ".wheels", ".git", "build"}
    csrc = []
    for d_, dn, fs in os.walk(src):
        dn[:] = [x for x in dn if x not in skipd and not x.startswith(".")]
        csrc += [os.path.relpath(os.path.join(d_, f_), src) for f_ in fs if f_.endswith((".pyx", ".c", ".cpp", ".cc"))]
        if len(csrc) > 200:
            break
    setup_ext = False
    for f_ in ("setup.py", "build.py"):
        try:
            setup_ext = setup_ext or bool(re.search(r"\bExtension\(|ext_modules|cythonize\(", open(os.path.join(src, f_), errors="replace").read()))
        except OSError:
            pass
    print("%s: compiled extensions installed: %d%s" % (me, len(sos), (" (" + ", ".join(sorted(sos)[:8]) + (", ..." if len(sos) > 8 else "") + ")") if sos else ""))
    ext_mode = E.get("UT_EXT", "auto")
    if ext_mode == "required" and not sos:
        why.append("extensions: required, but no compiled extension was installed (an optional extension failed to build?)")
    if ext_mode == "auto" and not sos and (setup_ext or any(c.endswith(".pyx") for c in csrc)):
        print("UPSTREAM-TESTS-WARN %s: the source has extension modules (%s) but no compiled extension was installed; an optional extension may have failed to build (look for 'Python.h: No such file' or 'Skipping optional' above)" % (
            pkg, "setup.py ext_modules" if setup_ext else ", ".join(sorted(c for c in csrc if c.endswith(".pyx"))[:5])))
# Compare the destdir snapshots (path size mtime mode) taken around
# the run (busybox has no normal-format diff, so do it here).
before = set(open(os.path.join(E["UT_SNAP"], "before")).read().splitlines())
after = set(open(os.path.join(E["UT_SNAP"], "after")).read().splitlines())
if before != after:
    bp = {l.rsplit(" ", 3)[0] for l in before}
    ap = {l.rsplit(" ", 3)[0] for l in after}
    ch = ["added: " + x for x in sorted(ap - bp)] + ["removed: " + x for x in sorted(bp - ap)]
    ch += ["changed: " + x for x in sorted({l.rsplit(" ", 3)[0] for l in after - before} & bp)]
    why.append("the run changed the installed package in %s (bytecode, caches or files written by tests): %s" % (
        E.get("UT_DEST", ""), "; ".join(ch[:20])))
if why:
    missing = set()
    blobs = [w for _, w in cerr] + list(r["skips"])
    for b in blobs:
        missing.update(re.findall(r"No module named '([A-Za-z0-9_]+)", b))
        missing.update(re.findall(r"could not import '([A-Za-z0-9_]+)", b))
    if missing:
        print("%s: HINT: tests could not import: %s; add the packages that provide them (guess: %s) to %s, or name the files in ignore-files" % (
            me, " ".join(sorted(missing)),
            " ".join("py3-supported-" + m.lower().replace("_", "-") for m in sorted(missing)), E["UT_ENVPKGS"]))
    print("UPSTREAM-TESTS %s: FAILED: %s" % (pkg, "; ".join(why)))
    sys.exit(1)
