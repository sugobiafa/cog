from glob import glob
import os
import subprocess

import requests

from .util import random_string, set_model_url, show_version, push_with_log


def test_build_show_list_download_infer(cog_server_port, project_dir, tmpdir_factory):
    user = random_string(10)
    model_name = random_string(10)
    model_url = f"http://localhost:{cog_server_port}/{user}/{model_name}"

    with open(os.path.join(project_dir, "cog.yaml")) as f:
        cog_yaml = f.read()

    set_model_url(model_url, project_dir)

    out, _ = subprocess.Popen(
        ["cog", "push"],
        cwd=project_dir,
        stdout=subprocess.PIPE,
    ).communicate()

    assert out.decode().startswith("Successfully uploaded version "), (
        out.decode() + " doesn't start with 'Successfully uploaded version'"
    )
    version_id = out.decode().strip().split("Successfully uploaded version ")[1]

    out, _ = subprocess.Popen(
        ["cog", "--model", model_url, "show", version_id], stdout=subprocess.PIPE
    ).communicate()
    lines = out.decode().splitlines()
    assert lines[0] == f"ID:       {version_id}"
    assert lines[1] == f"Model:    {user}/{model_name}"

    out = show_version(model_url, version_id)
    subprocess.Popen(
        ["cog", "--model", model_url, "build", "log", "-f", out["build_ids"]["cpu"]]
    ).communicate()

    out = show_version(model_url, version_id)
    assert out["config"]["examples"][2]["output"] == "@cog-example-output/output.02.txt"

    # show without --model
    out, _ = subprocess.Popen(
        ["cog", "show", version_id],
        stdout=subprocess.PIPE,
        cwd=project_dir,
    ).communicate()
    lines = out.decode().splitlines()
    assert lines[0] == f"ID:       {version_id}"
    assert lines[1] == f"Model:    {user}/{model_name}"

    out, _ = subprocess.Popen(
        ["cog", "--model", model_url, "ls"], stdout=subprocess.PIPE
    ).communicate()
    lines = out.decode().splitlines()
    assert lines[1].startswith(f"{version_id}  ")

    download_dir = tmpdir_factory.mktemp("download") / "my-dir"
    subprocess.Popen(
        [
            "cog",
            "--model",
            model_url,
            "download",
            "--output-dir",
            download_dir,
            version_id,
        ],
        stdout=subprocess.PIPE,
    ).communicate()
    paths = sorted(glob(str(download_dir / "*.*")))
    filenames = [os.path.basename(f) for f in paths]
    assert filenames == ["cog.yaml", "infer.py", "myfile.txt"]

    with open(download_dir / "cog-example-output/output.02.txt") as f:
        assert f.read() == "fooquxbaz"

    output_dir = tmpdir_factory.mktemp("output")
    input_path = output_dir / "input.txt"
    with input_path.open("w") as f:
        f.write("input")

    files_endpoint = f"http://localhost:{cog_server_port}/v1/models/{user}/{model_name}/versions/{version_id}/files"
    assert requests.get(f"{files_endpoint}/cog.yaml").text == cog_yaml
    assert (
        requests.get(f"{files_endpoint}/cog-example-output/output.02.txt").text
        == "fooquxbaz"
    )

    out_path = output_dir / "out.txt"
    subprocess.Popen(
        [
            "cog",
            "--model",
            model_url,
            "infer",
            "-o",
            out_path,
            "-i",
            "text=baz",
            "-i",
            f"path=@{input_path}",
            version_id,
        ],
        stdout=subprocess.PIPE,
    ).communicate()
    with out_path.open() as f:
        assert f.read() == "foobazinput"


def test_push_log(cog_server_port, project_dir):
    user = random_string(10)
    model_name = random_string(10)
    model_url = f"http://localhost:{cog_server_port}/{user}/{model_name}"

    set_model_url(model_url, project_dir)
    version_id = push_with_log(project_dir)

    out = show_version(model_url, version_id)
    assert out["config"]["examples"][2]["output"] == "@cog-example-output/output.02.txt"
    assert out["images"][0]["arch"] == "cpu"
    assert out["images"][0]["run_arguments"]["text"]["type"] == "str"
