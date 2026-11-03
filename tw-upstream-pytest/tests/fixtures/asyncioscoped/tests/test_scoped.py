import asyncio

import pytest
import pytest_asyncio

import demo


@pytest_asyncio.fixture(scope="module")
async def module_loop():
    return asyncio.get_running_loop()


@pytest_asyncio.fixture(scope="session")
async def session_loop():
    return asyncio.get_running_loop()


@pytest_asyncio.fixture(scope="module")
async def module_base():
    return await demo.double(10)


@pytest.mark.asyncio(loop_scope="module")
async def test_module_fixture_runs_in_module_loop(module_loop):
    assert asyncio.get_running_loop() is module_loop


@pytest.mark.asyncio(loop_scope="session")
async def test_session_fixture_runs_in_session_loop(session_loop):
    assert asyncio.get_running_loop() is session_loop


@pytest.mark.asyncio
async def test_module_value(module_base):
    assert module_base + 1 == 21


def test_loop_scope_unset(request):
    assert request.config.getini("asyncio_default_fixture_loop_scope") in ("", None)
