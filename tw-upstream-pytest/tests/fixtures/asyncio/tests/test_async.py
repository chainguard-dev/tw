import os

import pytest
import pytest_asyncio

import demo


@pytest_asyncio.fixture
async def base():
    return 21


@pytest.mark.asyncio
async def test_double(base):
    assert await demo.double(base) == 42


def test_loop_scope(request):
    # Unset (the tool only ignores the warning), or what WANT_LOOP_SCOPE
    # says the caller set.
    want = os.environ.get("WANT_LOOP_SCOPE", "")
    assert (request.config.getini("asyncio_default_fixture_loop_scope") or "") == want
