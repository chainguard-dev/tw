import configparser, os
LOOP = "asyncio_default_fixture_loop_scope"
KEYS = ("testpaths", "python_files", LOOP, "addopts")
def ini(f, sec):
    c = configparser.ConfigParser(interpolation=None)
    try:
        c.read(f)
    except Exception:
        return None
    if not c.has_section(sec):
        return None
    return {k: c.get(sec, k) for k in KEYS if c.has_option(sec, k)}
def table(f, *path):
    # The table at path in a TOML file: {} if it is missing or the file
    # cannot be parsed, None if there is no TOML parser (tomllib is 3.11+;
    # on 3.10 pytest itself needs tomli).
    try:
        import tomllib
    except ImportError:
        try:
            import tomli as tomllib
        except ImportError:
            return None
    try:
        with open(f, "rb") as fh:
            t = tomllib.load(fh)
        for k in path:
            t = t.get(k, {})
    except Exception:
        return {}
    return t if isinstance(t, dict) else {}
def strs(o):
    # TOML values as strings: a list (addopts, testpaths) joined with
    # spaces, anything else str().
    return {k: (" ".join(str(x) for x in o[k]) if isinstance(o[k], list) else str(o[k])) for k in KEYS if k in o}
def main():
    # pytest 9.2's discovery order (CONFIG_LOADERS_BY_NAME in
    # _pytest/config/findpaths.py); the first file with pytest config wins.
    # pytest.toml and .pytest.toml ([pytest] table, native TOML types) and
    # pytest.ini and .pytest.ini are the config even without that section.
    for f in ("pytest.toml", ".pytest.toml"):
        if os.path.isfile(f):
            return strs(table(f, "pytest") or {})
    for f in ("pytest.ini", ".pytest.ini"):
        if os.path.isfile(f):
            return ini(f, "pytest") or {}
    if os.path.isfile("pyproject.toml"):
        t = table("pyproject.toml", "tool", "pytest")
        if t:
            o = t.get("ini_options", t)
            return strs(o if isinstance(o, dict) else {})
    for f, sec in (("tox.ini", "pytest"), ("setup.cfg", "tool:pytest")):
        if os.path.isfile(f):
            v = ini(f, sec)
            if v is not None:
                return v
    return {}
d = main()
print(" ".join(d.get("testpaths", "").split()))
print("set" if "python_files" in d else "unset")
# pytest-asyncio's fixture loop scope, set as an ini key, and set in addopts
# (-o asyncio_default_fixture_loop_scope=...); the tool then passes no -W.
print("set" if d.get(LOOP, "").strip() else "unset")
print("set" if LOOP in d.get("addopts", "") else "unset")
# pytest-asyncio disabled in addopts (-p no:asyncio).
print("set" if "no:asyncio" in d.get("addopts", "") else "unset")
