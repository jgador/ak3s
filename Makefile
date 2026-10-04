PYTHON ?= .venv/bin/python
AK3S = $(PYTHON) scripts/ak3s.py

.PHONY: setup install validate bootstrap platform render status test
setup:
	python3 -m venv .venv
	.venv/bin/pip install -r requirements.txt

install validate bootstrap platform render status:
	$(AK3S) $@

test:
	$(PYTHON) -m unittest discover -s tests -v
	.venv/bin/ansible-playbook -i examples/inventory.yaml ansible/site.yaml --syntax-check
