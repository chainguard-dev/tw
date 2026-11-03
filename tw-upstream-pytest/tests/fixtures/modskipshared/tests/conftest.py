# Review of tw#504: two modules skip at collection through this one helper,
# so their skip reports have the same longrepr (this file, this line, the
# reason). xdist's controller passes on only the first report with a given
# longrepr; the plugin's per-worker record keeps both.
import pytest


def require_x():
    pytest.skip("fixture: no X", allow_module_level=True)
