import configparser, os
KEYS = ("testpaths", "python_files")
def ini(f, sec):
    c = configparser.ConfigParser(interpolation=None)
    try:
        c.read(f)
    except Exception:
        return None
    if not c.has_section(sec):
        return None
    return {k: c.get(sec, k) for k in KEYS if c.has_option(sec, k)}
def main():
    for f in ("pytest.ini", ".pytest.ini"):
        if os.path.isfile(f):
            return ini(f, "pytest") or {}
    if os.path.isfile("pyproject.toml"):
        try:
            import tomllib
        except ImportError:
            try:
                import tomli as tomllib
            except ImportError:
                tomllib = None
        if tomllib:
            try:
                t = tomllib.load(open("pyproject.toml", "rb")).get("tool", {}).get("pytest", {})
            except Exception:
                t = {}
            if t:
                o = t.get("ini_options", t)
                return {k: (" ".join(o[k]) if isinstance(o[k], list) else str(o[k])) for k in KEYS if k in o}
    for f, sec in (("tox.ini", "pytest"), ("setup.cfg", "tool:pytest")):
        if os.path.isfile(f):
            v = ini(f, sec)
            if v is not None:
                return v
    return {}
d = main()
print(" ".join(d.get("testpaths", "").split()))
print("set" if "python_files" in d else "unset")
