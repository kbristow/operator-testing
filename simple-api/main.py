import json

from fastapi import FastAPI, Response, status

app = FastAPI()


@app.get("/{config_name}", status_code=200)
async def root(config_name: str, response: Response):
    try:
        with open(config_name + ".json") as fh:
            response = json.loads(fh.read())

        return response
    except:
        response.status_code = status.HTTP_404_NOT_FOUND
        return {}
