FROM python:3

RUN mkdir -p /workplace/app
ADD main.py /workplace/app
ADD requirements.txt /workplace/app

WORKDIR /workplace/app
RUN pip install -r requirements.txt

CMD ["python", "main.py"]
