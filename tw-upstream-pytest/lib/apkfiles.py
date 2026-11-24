"""The test stage's view of the package under test (contract 5).

  apkfiles.py list PKG SITE_PACKAGES OUTDIR ME
      Reads PKG's files from the apk database (what `apk info -L PKG`
      prints; the database is read directly, so apk-tools need not be in
      the test environment), checks that every one is installed and still
      matches the database's checksum, and writes:
        OUTDIR/files    the absolute paths, one per line
        OUTDIR/entries  the top-level names PKG owns in SITE_PACKAGES
      It fails (exit 1, with a message) when PKG is not installed, lists
      no files, has a missing or modified file, or installs nothing in
      SITE_PACKAGES (a metapackage, or another python's subpackage).
  apkfiles.py snapshot FILES
      Prints one line per path, sorted: every file in FILES, and every
      entry of each directory that holds one of them (so that a file a
      test adds next to the package's files is seen):
        path size mtime_ns mode sha256   (regular files)
        path size mtime_ns mode -        (links: size is the target's length)
        path 0 0 mode -                  (directories)
      Taken before and after the run; report.py compares them.

The database is UT_APK_DB if set (tests/run.sh points it at a fake one),
else /usr/lib/apk/db/installed, else /lib/apk/db/installed.
"""
import base64, hashlib, os, re, stat, sys

DBS = ("/usr/lib/apk/db/installed", "/lib/apk/db/installed")


def db_path():
    p = os.environ.get("UT_APK_DB")
    if p:
        return p
    for p in DBS:
        if os.path.isfile(p):
            return p
    return DBS[0]


def read_db(path, pkg):
    """[(absolute path, checksum or None)] for pkg, or None if it is not
    in the database. A block per package, separated by blank lines; F: is
    a directory (relative to /), R: a file in the last F:, Z: the checksum
    of the last R:."""
    found, files, d, last = False, [], None, None
    with open(path, encoding="utf-8", errors="surrogateescape") as fh:
        for line in fh:
            line = line.rstrip("\n")
            if not line:
                if found:
                    break
                d = last = None
                continue
            k, v = line[:2], line[2:]
            if k == "P:":
                found = v == pkg
            if not found:
                continue
            if k == "F:":
                d, last = v, None
            elif k == "R:":
                files.append(["/" + (d + "/" if d else "") + v, None])
                last = files[-1]
            elif k == "Z:" and last is not None:
                last[1] = v
    return files if found else None


def checksum_ok(p, z):
    """True or False when the database's checksum can be checked, None when
    its format is not known. Q1: base64 SHA-1, Q2: base64 SHA-256, of the
    file's contents or a link's target (apk-tools)."""
    algo = {"Q1": hashlib.sha1, "Q2": hashlib.sha256}.get(z[:2])
    if algo is None:
        return None
    if os.path.islink(p):
        data = os.readlink(p).encode("utf-8", "surrogateescape")
    else:
        with open(p, "rb") as fh:
            data = fh.read()
    return base64.b64encode(algo(data).digest()).decode() == z[2:]


def cmd_list(pkg, sp, out, me):
    db = db_path()
    if not os.path.isfile(db):
        sys.exit("%s: no apk database at %s: the test stage tests a package apk installed" % (me, db))
    files = read_db(db, pkg)
    if files is None:
        sys.exit("%s: package '%s' is not installed (not in %s); name the (sub)package under test, installed in the test environment" % (me, pkg, db))
    if not files:
        sys.exit("%s: apk lists no files for '%s' (a metapackage?); name the subpackage that installs the Python files" % (me, pkg))
    missing, bad, unknown = [], [], 0
    for p, z in files:
        if not os.path.lexists(p):
            missing.append(p)
        elif z and (os.path.islink(p) or stat.S_ISREG(os.lstat(p).st_mode)):
            ok = checksum_ok(p, z)
            if ok is None:
                unknown += 1
            elif not ok:
                bad.append(p)
    if missing:
        sys.exit("%s: %d file(s) of '%s' listed in %s are missing: %s" % (me, len(missing), pkg, db, " ".join(missing[:20])))
    if bad:
        sys.exit("%s: %d installed file(s) of '%s' differ from the apk database (modified before the run): %s" % (me, len(bad), pkg, " ".join(bad[:20])))
    spr = os.path.realpath(sp)
    entries, n = set(), 0
    for p, _ in files:
        for base in (sp, spr):
            if p.startswith(base + "/"):
                entries.add(p[len(base) + 1:].split("/")[0])
                n += 1
                break
    if not entries:
        other = sorted({m.group(0) for p, _ in files for m in [re.search(r"^.*/python3\.[0-9]+/site-packages", p)] if m})
        sys.exit("%s: '%s' installs nothing in %s%s; set python to the interpreter of the package under test" % (
            me, pkg, sp, " (it installs into %s)" % ", ".join(other) if other else ""))
    with open(os.path.join(out, "files"), "w", encoding="utf-8", errors="surrogateescape") as fh:
        fh.writelines(p + "\n" for p, _ in files)
    with open(os.path.join(out, "entries"), "w", encoding="utf-8", errors="surrogateescape") as fh:
        fh.writelines(e + "\n" for e in sorted(entries))
    nz = sum(1 for _, z in files if z)
    print("%s: stage test: testing the installed package %s: %d files from %s (%d in %s), %d checksum(s) match the database%s" % (
        me, pkg, len(files), db, n, sp, nz - unknown, "; %d in an unknown format, not checked" % unknown if unknown else ""))


def line(p):
    try:
        st = os.lstat(p)
    except FileNotFoundError:
        return None
    if stat.S_ISDIR(st.st_mode):
        return "%s 0 0 %o -" % (p, st.st_mode)
    h = "-"
    if stat.S_ISREG(st.st_mode):
        d = hashlib.sha256()
        with open(p, "rb") as fh:
            for b in iter(lambda: fh.read(1 << 20), b""):
                d.update(b)
        h = d.hexdigest()
    return "%s %d %d %o %s" % (p, st.st_size, st.st_mtime_ns, st.st_mode, h)


def cmd_snapshot(listing):
    with open(listing, encoding="utf-8", errors="surrogateescape") as fh:
        files = [l.rstrip("\n") for l in fh if l.strip()]
    paths = set(files)
    for d in {os.path.dirname(p) for p in files}:
        try:
            paths.update(os.path.join(d, e) for e in os.listdir(d))
        except FileNotFoundError:
            pass
        paths.add(d)
    out = [l for l in map(line, sorted(paths)) if l is not None]
    sys.stdout.write("".join(l + "\n" for l in out))


if __name__ == "__main__":
    if len(sys.argv) == 6 and sys.argv[1] == "list":
        cmd_list(*sys.argv[2:])
    elif len(sys.argv) == 3 and sys.argv[1] == "snapshot":
        cmd_snapshot(sys.argv[2])
    else:
        sys.exit("usage: apkfiles.py list PKG SITE_PACKAGES OUTDIR ME | snapshot FILES")
