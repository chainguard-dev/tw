import errno, glob, importlib.util, json, os, re, socket, sys
try:
    import pytest
    hookimpl = pytest.hookimpl
except ImportError:  # loaded by the unittest runner without pytest
    pytest = None
    hookimpl = lambda **k: (lambda f: f)

OUT = os.environ["UT_OUT"]
DSP = os.environ.get("UT_DSP", "")
FROM = os.environ.get("UT_FROM", "installed")
SRC = os.environ.get("UT_SRC", "")
results, collect_errors, deselected, skips = {}, [], [], {}
RANK = {"passed": 0, "skipped": 1, "xfailed": 2, "failed": 4, "error": 5}

if FROM == "installed" and DSP:
    import site
    site.addsitedir(DSP)  # .pth files

def _front():
    # The destdir goes first, ahead of the project's pytest
    # `pythonpath` entries (inserted before pytest_configure).
    if FROM == "installed" and DSP:
        while DSP in sys.path:
            sys.path.remove(DSP)
        sys.path.insert(0, DSP)

TESTISH = re.compile(r"^(_?tests?|testing|unittests?|conftest|test_.*|.*_tests?)$")

def testish(name):
    # A module that only exists for the test suite: some dotted component
    # is tests/test/testing/conftest, test_*, *_test or *_tests.
    return any(TESTISH.match(p) for p in name.split(".")[1:])

class _PinInstalled(object):
    """First finder (installed mode) for the package's own top-level
    modules: they always resolve to the destdir, even when a test or
    conftest puts the source tree first on sys.path (a common upstream
    habit), or import-mode is prepend. Their submodules follow the
    package's __path__ (the destdir). Namespace packages (no __init__.py)
    are left alone, so other distributions' portions stay importable."""
    tops = set()

    @classmethod
    def setup(cls):
        if not (DSP and os.path.isdir(DSP)):
            return
        for e in os.listdir(DSP):
            if e.endswith((".dist-info", ".egg-info", ".pth", ".data")) or e.startswith(("__pycache__", "_distutils_hack")):
                continue
            p = os.path.join(DSP, e)
            if os.path.isdir(p) and not os.path.isfile(os.path.join(p, "__init__.py")):
                continue
            if not os.path.isdir(p) and not e.endswith((".py", ".so", ".pyd")):
                continue
            name = e.split(".")[0]
            if name not in ("tests", "test", "testing", "conftest", "docs", "benchmarks", "examples"):
                cls.tops.add(name)
        if cls.tops:
            sys.meta_path.insert(0, cls())

    def find_spec(self, fullname, path=None, target=None):
        if fullname not in self.tops:
            return None
        import importlib.machinery
        return importlib.machinery.PathFinder.find_spec(fullname, [DSP])

TESTPKG = re.compile(r"^(_?tests?|unittests?|testsuite|test_.*|.*_tests?)$")

class _SourceTestPkgs(object):
    """Front finder (installed mode, right after the destdir pin) for the
    project's own test packages: dotted names with a tests/test/_tests
    component or a test_*/*_test leaf are loaded from the source tree when
    the source has them, even if the wheel ships a copy too. The tests then
    run as collected (no import-file mismatch), files they write next to
    themselves land in the scratch tree, not in the package, and data the
    wheel does not ship is found; the code under test still comes from the
    destdir (the top-level package is pinned there). `testing` modules are
    not included: they are often public API (numpy.testing)."""

    def __init__(self, roots):
        self.roots = roots

    def find_spec(self, fullname, path=None, target=None):
        parts = fullname.split(".")
        root = self.roots.get(parts[0])
        if len(parts) < 2 or not root or not any(TESTPKG.match(p) for p in parts[1:]):
            return None
        base = os.path.join(root, *parts[1:])
        if os.path.isfile(os.path.join(base, "__init__.py")):
            return importlib.util.spec_from_file_location(
                fullname, os.path.join(base, "__init__.py"), submodule_search_locations=[base])
        if os.path.isfile(base + ".py"):
            # Let pytest's assertion rewriter handle test modules.
            for f in sys.meta_path:
                if type(f).__name__ == "AssertionRewritingHook":
                    try:
                        sp = f.find_spec(fullname, [os.path.dirname(base)])
                    except Exception:
                        sp = None
                    if sp is not None and os.path.realpath(sp.origin or "") == os.path.realpath(base + ".py"):
                        return sp
            return importlib.util.spec_from_file_location(fullname, base + ".py")
        return None

class _SourceTests(object):
    """Last-resort finder (installed mode) for test-only submodules of the
    package under test that the wheel does not ship (foo/tests/ excluded
    from the wheel): foo is imported from the destdir, and foo.tests (and
    only test-ish names) from the source tree. Installed modules always
    win, as this finder runs after the normal ones."""
    roots = {}

    @classmethod
    def setup(cls):
        if not (DSP and SRC and os.path.isdir(DSP)):
            return
        skip = {"melange-out", ".wheels", ".git", "build", "node_modules", ".tox", ".venv"}
        for top in os.listdir(DSP):
            if not os.path.isdir(os.path.join(DSP, top)) or top.endswith((".dist-info", ".egg-info", ".data")) or top == "__pycache__":
                continue
            found = None
            for depth in range(0, 3):
                pats = [os.path.join(SRC, *(["*"] * depth), top)]
                for pat in pats:
                    for c in sorted(glob.glob(pat)):
                        rel = os.path.relpath(c, SRC).split(os.sep)
                        if os.path.isdir(c) and not (set(rel) & skip):
                            found = c
                            break
                    if found:
                        break
                if found:
                    break
            if found:
                cls.roots[top] = found
        if cls.roots:
            sys.meta_path.append(cls())
            pos = 1 if sys.meta_path and isinstance(sys.meta_path[0], _PinInstalled) else 0
            sys.meta_path.insert(pos, _SourceTestPkgs(cls.roots))

    def find_spec(self, fullname, path=None, target=None):
        parts = fullname.split(".")
        root = self.roots.get(parts[0])
        if len(parts) < 2 or not root or not testish(fullname):
            return None
        base = os.path.join(root, *parts[1:])
        if os.path.isfile(os.path.join(base, "__init__.py")):
            return importlib.util.spec_from_file_location(
                fullname, os.path.join(base, "__init__.py"), submodule_search_locations=[base])
        if os.path.isfile(base + ".py"):
            return importlib.util.spec_from_file_location(fullname, base + ".py")
        return None

    def find_module(self, fullname, path=None):  # pragma: no cover (py2-style API)
        return None

def _no(path, err, why):
    NOTES.append("import of %s (%s) not retried: %s" % (path, err, why))
    return None

def _collision_retry(path, err):
    # A source-tree test package whose top-level name is taken by
    # something outside the source tree (the stdlib's `test`, a stray
    # top-level `tests` package some wheel installed): append mode finds
    # the other one first ("'test' is not a package", "No module named
    # 'tests.test_x'"). Import it with its root first, for this import
    # only, as prepend mode would. Never for the package under test.
    try:
        import importlib
        srcr = os.path.realpath(SRC) + os.sep if SRC else None
        want = os.path.realpath(str(path))
        if not srcr or not want.startswith(srcr):
            return _no(path, err, "not in the source tree")
        parts = []
        d = os.path.dirname(want)
        if os.path.basename(want) != "__init__.py":
            parts.append(os.path.splitext(os.path.basename(want))[0])
        while os.path.isfile(os.path.join(d, "__init__.py")):
            parts.insert(0, os.path.basename(d))
            d = os.path.dirname(d)
        if len(parts) < 2 or parts[0] in _PinInstalled.tops:
            return _no(path, err, "not a dotted test package name (%s)" % ".".join(parts))
        top, name, root = parts[0], ".".join(parts), d
        missing = getattr(err, "name", None) or ""
        if "is not a package" not in str(err) and not (missing and (name == missing or name.startswith(missing + "."))):
            return None  # an ordinary missing dependency, not a name collision
        other = sys.modules.get(top)
        of = os.path.realpath(getattr(other, "__file__", "") or "") if other else ""
        if other is not None and of.startswith(srcr):
            return _no(path, err, "%r already comes from the source tree (%s)" % (top, of))
        saved = {k: v for k, v in sys.modules.items() if k == top or k.startswith(top + ".")}
        for k_ in saved:
            del sys.modules[k_]
        sys.path.insert(0, root)
        try:
            importlib.invalidate_caches()
            mod = importlib.import_module(name)
        finally:
            sys.path.remove(root)
        if os.path.realpath(getattr(mod, "__file__", "") or "") == want:
            NOTES.append("%s: imported with its root first (top-level %r was %s)" % (name, top, of or "unset"))
            return mod
        NOTES.append("%s: retry imported %s, not %s" % (name, getattr(mod, "__file__", None), want))
        sys.modules.update(saved)
    except Exception as x:
        NOTES.append("import of %s (%s) not retried: %r" % (path, err, x))
        return None
    NOTES.append("import of %s (%s) not retried" % (path, err))
    return None

def _accept_installed_copy():
    """Tests shipped in the wheel (foo/tests/): pytest imports the source
    tree's foo/tests/test_x.py as foo.tests.test_x, which resolves to the
    destdir copy, and stops with an "import file mismatch". When the two
    files are byte-identical, use the installed module: the same test code,
    importing the installed package. Different content is still an error."""
    try:
        import _pytest.pathlib as pl
    except Exception:
        return
    orig = pl.import_path

    def same(a, b):
        try:
            with open(a, "rb") as x, open(b, "rb") as y:
                return x.read() == y.read()
        except OSError:
            return False

    def import_path(path, *a, **k):
        try:
            return orig(path, *a, **k)
        except pl.ImportPathMismatchError as e:
            name, modfile, want = (list(e.args) + [None, None, None])[:3]
            modfile = os.path.realpath(str(modfile))
            if DSP and modfile.startswith(os.path.realpath(DSP) + os.sep) and same(modfile, str(want)) and name in sys.modules:
                return sys.modules[name]
            # The other direction: pytest collects the destdir copy (`--pyargs`,
            # testpaths into the package) while the test package was loaded from
            # the source tree: identical files, same test code.
            if DSP and SRC and os.path.realpath(str(want)).startswith(os.path.realpath(DSP) + os.sep) and modfile.startswith(os.path.realpath(SRC) + os.sep) and same(modfile, str(want)) and name in sys.modules:
                return sys.modules[name]
            # Two source-tree files with the same top-level name (conftest.py in
            # directories without __init__.py, test_utils.py twice): append mode
            # adds each directory at the end of sys.path, so the first one always
            # wins. Import the wanted one with its directory first, for this import
            # only (what prepend mode does); the package under test stays pinned.
            srcr = os.path.realpath(SRC) + os.sep if SRC else None
            wantr = os.path.realpath(str(want))
            if srcr and "." not in str(name) and modfile.startswith(srcr) and wantr.startswith(srcr):
                d = os.path.dirname(wantr)
                old_mod = sys.modules.pop(name, None)
                sys.path.insert(0, d)
                try:
                    import importlib
                    importlib.invalidate_caches()
                    mod = importlib.import_module(name)
                finally:
                    sys.path.remove(d)
                if os.path.realpath(getattr(mod, "__file__", "") or "") == wantr:
                    return mod
                if old_mod is not None:
                    sys.modules[name] = old_mod
            raise
        except ImportError as e:
            m = _collision_retry(path, e)
            if m is None:
                raise
            return m

    for mname, m in list(sys.modules.items()):
        if (mname == "_pytest" or mname.startswith("_pytest.")) and getattr(m, "import_path", None) is orig:
            m.import_path = import_path

if FROM == "installed":
    _accept_installed_copy()
    # At import (-p loads this plugin before any conftest), so that a
    # root conftest that puts the source tree on sys.path is covered too.
    _PinInstalled.setup()
    _SourceTests.setup()

_names = []

def _local_names():
    # The local hostname and FQDN, looked up once with the real functions
    # before the guard replaces them (getfqdn calls gethostbyaddr).
    if not _names:
        n = set()
        for f in (socket.gethostname, socket.getfqdn):
            try:
                n.add(f())
            except Exception:
                pass
        _names.append(n)
    return _names[0]

def _local(host):
    if host in (None, "", "localhost", "localhost.localdomain", "ip6-localhost"):
        return True
    if isinstance(host, bytes):
        host = host.decode("ascii", "replace")
    h = str(host).split("%")[0]
    if h.startswith("127.") or h in ("::1", "0.0.0.0", "::"):
        return True
    return h in _local_names()

def _guard():
    if os.environ.get("UT_NONET") != "1":
        return
    import weakref
    _local_names()
    real_connect = socket.socket.connect
    real_connect_ex = socket.socket.connect_ex
    real_gai = socket.getaddrinfo
    real_ghbn, real_ghbn_ex = socket.gethostbyname, socket.gethostbyname_ex
    real_ghba, real_gni = socket.gethostbyaddr, socket.getnameinfo
    real_send, real_sendto = socket.socket.send, socket.socket.sendto
    real_sendall, real_sendmsg = socket.socket.sendall, getattr(socket.socket, "sendmsg", None)
    remote_dgram = weakref.WeakSet()

    def is_ip(h):
        for fam in (socket.AF_INET, socket.AF_INET6):
            try:
                socket.inet_pton(fam, str(h).split("%")[0])
                return True
            except (OSError, ValueError):
                pass
        return False

    def check(sock, addr):
        # None if allowed, else the exception a host without network gives.
        if getattr(socket, "AF_UNIX", None) == sock.family or not isinstance(addr, tuple) or _local(addr[0]):
            return None
        if not is_ip(addr[0]):
            # A name: the resolver fails first, as it would offline.
            return socket.gaierror(socket.EAI_NONAME, "upstream-tests: network disabled (%r)" % (addr[0],))
        return OSError(errno.ENETUNREACH, "upstream-tests: network disabled (%r)" % (addr,))

    def connect(self, addr):
        e = check(self, addr)
        if e is not None:
            if self.type == socket.SOCK_DGRAM and not isinstance(e, socket.gaierror):
                # UDP connect() sends nothing; it is the usual way to find
                # the local address (connect to 8.8.8.8, getsockname()).
                # Sending through such a socket is refused below.
                r = real_connect(self, addr)
                remote_dgram.add(self)
                return r
            raise e
        return real_connect(self, addr)

    def connect_ex(self, addr):
        e = check(self, addr)
        if e is not None:
            if self.type == socket.SOCK_DGRAM and not isinstance(e, socket.gaierror):
                remote_dgram.add(self)
                return real_connect_ex(self, addr)
            return errno.ENETUNREACH if not isinstance(e, socket.gaierror) else e.errno
        return real_connect_ex(self, addr)

    def send(self, data, *a):
        if self in remote_dgram:
            raise OSError(errno.ENETUNREACH, "upstream-tests: network disabled (UDP send)")
        return real_send(self, data, *a)

    def sendto(self, data, *a):
        addr = a[-1] if a else None
        if self in remote_dgram or (addr is not None and check(self, addr) is not None):
            raise OSError(errno.ENETUNREACH, "upstream-tests: network disabled (%r)" % (addr,))
        return real_sendto(self, data, *a)

    def sendall(self, data, *a):
        if self in remote_dgram:
            raise OSError(errno.ENETUNREACH, "upstream-tests: network disabled (UDP sendall)")
        return real_sendall(self, data, *a)

    def sendmsg(self, buffers, *a):
        addr = a[2] if len(a) > 2 else None
        if self in remote_dgram or (addr is not None and check(self, addr) is not None):
            raise OSError(errno.ENETUNREACH, "upstream-tests: network disabled (%r)" % (addr,))
        return real_sendmsg(self, buffers, *a)

    def noname(host):
        return socket.gaierror(socket.EAI_NONAME, "upstream-tests: network disabled (%r)" % (host,))

    # Every resolver entry point of the socket module refuses names and
    # addresses other than loopback and the local hostname, as a host
    # without network would (gaierror EAI_NONAME; herror HOST_NOT_FOUND
    # for gethostbyaddr), so well-behaved suites skip.
    def getaddrinfo(host, *a, **k):
        if not _local(host):
            raise noname(host)
        return real_gai(host, *a, **k)

    def gethostbyname(host):
        if not _local(host):
            raise noname(host)
        return real_ghbn(host)

    def gethostbyname_ex(host):
        if not _local(host):
            raise noname(host)
        return real_ghbn_ex(host)

    def gethostbyaddr(host):
        if not _local(host):
            raise socket.herror(1, "upstream-tests: network disabled (%r)" % (host,))
        return real_ghba(host)

    def getnameinfo(sockaddr, flags):
        # NI_NUMERICHOST only formats the address (no lookup).
        host = sockaddr[0] if isinstance(sockaddr, tuple) and sockaddr else None
        if not _local(host) and not flags & socket.NI_NUMERICHOST:
            raise noname(host)
        return real_gni(sockaddr, flags)

    socket.socket.connect = connect
    socket.socket.connect_ex = connect_ex
    socket.socket.send = send
    socket.socket.sendto = sendto
    socket.socket.sendall = sendall
    if real_sendmsg is not None:
        socket.socket.sendmsg = sendmsg
    socket.getaddrinfo = getaddrinfo
    socket.gethostbyname = gethostbyname
    socket.gethostbyname_ex = gethostbyname_ex
    socket.gethostbyaddr = gethostbyaddr
    socket.getnameinfo = getnameinfo

# Options that upstream addopts commonly pass to plugins a build gate does
# not need (coverage, benchmarks, linters, reporting) or that this step
# replaces (xdist, timeout). When the plugin is not installed, the option is
# accepted and ignored instead of failing with "unrecognized arguments".
# Only options that actually appear on the command line/addopts are added.
STUBBED = []
NOTES = []
STUB_OPTS = {
    "pytest_cov": (["--cov", "--cov-report", "--cov-config", "--cov-fail-under", "--cov-context", "--cov-precision"],
                   ["--cov-branch", "--no-cov", "--no-cov-on-fail", "--cov-append", "--cov-reset"]),
    "xdist": (["-n", "--numprocesses", "--dist", "--maxprocesses", "--max-worker-restart", "--maxschedchunk"], ["--looponfail"]),
    "pytest_timeout": (["--timeout", "--timeout-method"], []),
    "pytest_benchmark": (["--benchmark-columns", "--benchmark-sort", "--benchmark-group-by", "--benchmark-min-rounds",
                          "--benchmark-max-time", "--benchmark-warmup", "--benchmark-storage", "--benchmark-save",
                          "--benchmark-json", "--benchmark-histogram", "--benchmark-timer", "--benchmark-calibration-precision",
                          "--benchmark-min-time", "--benchmark-warmup-iterations", "--benchmark-name", "--benchmark-cprofile"],
                         ["--benchmark-skip", "--benchmark-disable", "--benchmark-only", "--benchmark-autosave",
                          "--benchmark-enable", "--benchmark-disable-gc", "--benchmark-verbose", "--benchmark-quiet"]),
    "pytest_html": (["--html", "--css"], ["--self-contained-html"]),
    "pytest_flake8": ([], ["--flake8"]),
    "pytest_mypy": ([], ["--mypy", "--mypy-ignore-missing-imports"]),
    "pytest_black": ([], ["--black"]),
    "pytest_isort": ([], ["--isort"]),
    "pytest_pylint": ([], ["--pylint"]),
    "pytest_ruff": ([], ["--ruff", "--ruff-format"]),
    "pytest_pycodestyle": ([], ["--pycodestyle"]),
    "pytest_checkdocs": ([], ["--checkdocs"]),
    "pytest_instafail": ([], ["--instafail"]),
    "pytest_sugar": ([], ["--force-sugar"]),
    "pytest_rerunfailures": (["--reruns", "--reruns-delay", "--only-rerun"], []),
    "pytest_randomly": (["--randomly-seed"], ["--randomly-dont-reorganize", "--randomly-dont-reset-seed"]),
    "pytest_forked": ([], ["--forked"]),
    "pytest_remotedata": (["--remote-data"], []),
    "pytest_asyncio": (["--asyncio-mode", "--asyncio-debug"], []),
    "syrupy": (["--snapshot-default-extension", "--snapshot-dirname", "--snapshot-patch-pycharm-diff"], ["--snapshot-update", "--snapshot-warn-unused", "--snapshot-details"]),
    "pytest_mpl": (["--mpl-baseline-path", "--mpl-generate-path", "--mpl-results-path", "--mpl-hash-library", "--mpl-default-style", "--mpl-default-tolerance"], ["--mpl", "--mpl-generate-summary"]),
    "pytest_django": (["--ds", "--dc"], ["--reuse-db", "--create-db", "--nomigrations", "--migrations", "--no-migrations"]),
    "pytest_socket": (["--allow-hosts"], ["--disable-socket", "--allow-unix-socket"]),
}

# ini keys of plugins that are not installed: unknown keys are a
# PytestConfigWarning, which upstream filterwarnings=error/--strict-config
# turn into an internal error before anything runs ("Unknown config
# option: asyncio_mode"). Registered, they are inert; the tests that need
# the plugin then fail on their own, visibly.
STUB_INI = {
    "pytest_asyncio": ["asyncio_mode", "asyncio_default_fixture_loop_scope", "asyncio_default_test_loop_scope"],
    "pytest_mock": ["mock_use_standalone_module", "mock_traceback_monkeypatch"],
    "pytest_trio": ["trio_mode", "trio_run"],
    "pytest_env": ["env"],
    "pytest_django": ["DJANGO_SETTINGS_MODULE", "django_find_project"],
}

def pytest_addoption(parser):
    import importlib.util as iu
    for mod, keys in STUB_INI.items():
        try:
            if iu.find_spec(mod) is not None:
                continue
        except Exception:
            pass
        for k in keys:
            try:
                parser.addini(k, "(registered by upstream-tests/pytest; %s is not installed)" % mod, default=None)
            except Exception:
                pass

@hookimpl(tryfirst=True)
def pytest_load_initial_conftests(early_config, parser, args):
    import importlib.util as iu
    seen = {a.split("=", 1)[0] for a in args if isinstance(a, str) and a.startswith("-")}
    if any(isinstance(a, str) and re.match(r"^-n(\d+|auto|logical)$", a) for a in args):
        seen.add("-n")  # -n8, -nauto
    stubbed = []
    # _addoption: pytest reserves lowercase short options (-n) for itself
    # in addoption(); plugins such as xdist register them this way.
    grp_ = parser.getgroup("upstream-tests")
    for mod, (valued, flags) in STUB_OPTS.items():
        try:
            sp_ = iu.find_spec(mod)
            if sp_ is not None and "/stubs/" not in (sp_.origin or ""):
                continue
        except Exception:
            pass
        for o in valued + flags:
            if o not in seen:
                continue
            try:
                if o in valued:
                    grp_._addoption(o, dest="ut_stub_" + o.strip("-").replace("-", "_"), nargs="?", const=True,
                                     action="append" if o.startswith("--cov") else "store", help="(ignored: %s is not installed)" % mod)
                else:
                    grp_._addoption(o, dest="ut_stub_" + o.strip("-").replace("-", "_"), action="store_true",
                                     help="(ignored: %s is not installed)" % mod)
                stubbed.append(o)
            except Exception:
                pass
    STUBBED.extend(stubbed)

running = [None]

def _dump(exitstatus, collected, killed=None):
    with open(os.path.join(OUT, "results.json"), "w") as fh:
        json.dump({"results": results, "collect_errors": collect_errors,
                   "deselected": deselected, "skips": skips,
                   "collected": collected, "killed_during": killed, "why": why_failed,
                   "exitstatus": int(exitstatus), "stubbed": STUBBED, "notes": NOTES[:50]}, fh)

def _on_term(signum, frame):
    # overall-timeout: keep what ran so far and say which test was running.
    try:
        _dump_mods("main")
        _dump(128 + signum, len(results), running[0] or "(between tests)")
    finally:
        os._exit(128 + signum)

def pytest_runtest_logstart(nodeid, location):
    running[0] = nodeid

def pytest_runtest_logfinish(nodeid, location):
    running[0] = None

RESOURCE_DIRS = {"resources", "resource", "data", "testdata", "test_data", "fixtures", "samples", "sample", "examples", "example", "files", "corpus"}

def pytest_collect_file(file_path, parent):
    # Unittest-style suites often keep everything in test.py/tests.py,
    # which pytest's default python_files does not match. Collect such a
    # file when it looks like a test module (imports unittest/pytest or
    # defines test functions/classes) and does not live in a data
    # directory (py3-puremagic-2 has test/resources/system/test.py as a
    # sample file). Only when the project does not set python_files.
    if os.environ.get("UT_EXTRA_TESTFILES") != "1" or file_path.name not in ("test.py", "tests.py"):
        return None
    if RESOURCE_DIRS & set(p.lower() for p in file_path.parent.parts):
        return None
    try:
        head = file_path.read_text(errors="replace")[:200000]
    except OSError:
        return None
    if not re.search(r"^\s*(import unittest|from unittest|import pytest|from pytest|def test_?\w*\(|class Test\w*)", head, re.M):
        return None
    for pat in parent.config.getini("python_files"):
        if file_path.match(pat):
            return None  # pytest collects it already
    return pytest.Module.from_parent(parent, path=file_path)

def pytest_configure(config):
    _guard()
    _front()
    # Markers of plugins that are not installed (flaky reruns, ordering,
    # repeat, ...): under --strict-markers an unregistered marker is a
    # collection error. Registered, they are inert (a flaky test runs once).
    known = set(m.split(":")[0].split("(")[0].strip() for m in config.getini("markers"))
    for m in ("flaky", "repeat", "order", "run", "dependency", "xdist_group", "timeout", "asyncio", "anyio", "trio", "benchmark", "forked", "no_cover", "randomize", "mypy_testing"):
        if m not in known:
            config.addinivalue_line("markers", "%s: (registered by upstream-tests/pytest; plugin not installed)" % m)
    if not hasattr(config, "workerinput"):
        import signal
        signal.signal(signal.SIGTERM, _on_term)

def pytest_collection(session):
    _front()

def pytest_deselected(items):
    deselected.extend(i.nodeid for i in items)

def pytest_collectreport(report):
    # One entry per node id (--doctest-modules collects a file twice).
    if report.failed and (report.nodeid or "<session>") not in [n for n, _ in collect_errors]:
        collect_errors.append((report.nodeid or "<session>", str(report.longrepr)[-4000:]))

def _set(nodeid, outcome):
    if RANK[outcome] >= RANK.get(results.get(nodeid), -1):
        results[nodeid] = outcome

finished, occ, cur, why_failed = set(), {}, {}, {}

def pytest_runtest_logreport(report):
    if report.outcome == "rerun":
        return
    # Some suites generate the same node id twice (duplicate
    # parametrize ids); count each run, as pytest's summary does.
    n = report.nodeid
    if report.when == "setup":
        if n in finished:
            occ[n] = occ.get(n, 1) + 1
            finished.discard(n)
        cur[n] = n if n not in occ else "%s#%d" % (n, occ[n])
    k = cur.get(n, n)
    if report.when == "teardown":
        finished.add(n)
    xf = hasattr(report, "wasxfail")
    if report.when == "call":
        if report.passed:
            _set(k, "passed")
        elif report.failed:
            _set(k, "failed")
        elif report.skipped:
            _set(k, "xfailed" if xf else "skipped")
    elif report.failed:
        _set(k, "error")
    elif report.when == "setup" and report.skipped:
        _set(k, "xfailed" if xf else "skipped")
    if report.failed and k not in why_failed:
        rc_ = getattr(getattr(report, "longrepr", None), "reprcrash", None)
        why_failed[k] = (getattr(rc_, "message", None) or str(report.longrepr).strip().splitlines()[-1:] or [""])
        if isinstance(why_failed[k], list):
            why_failed[k] = why_failed[k][0] if why_failed[k] else ""
        why_failed[k] = " ".join(str(why_failed[k]).split())[:300]
    if report.skipped and not xf and isinstance(report.longrepr, tuple):
        r = str(report.longrepr[2])
        skips[r] = skips.get(r, 0) + 1

def _dump_mods(wid):
    mods = {}
    # Every module with a file (dotted names too, so namespace
    # packages such as jaraco.* are seen through their submodules).
    for name, m in list(sys.modules.items()):
        f = getattr(m, "__file__", None)
        if isinstance(f, str):
            mods[name] = os.path.realpath(f)
    with open(os.path.join(OUT, "modules-%s.json" % wid), "w") as fh:
        json.dump(mods, fh)

def pytest_sessionfinish(session, exitstatus):
    wid = getattr(session.config, "workerinput", {}).get("workerid", "main")
    _dump_mods(wid)
    if wid != "main":
        return
    _dump(exitstatus, session.testscollected)
