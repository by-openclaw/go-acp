#!/bin/bash
echo "--- form actions on AMWA / page ---"
curl -s http://localhost:5001/ | grep -oE 'action="[^"]+"' | sort -u
echo "--- input names ---"
curl -s http://localhost:5001/ | grep -oE 'name="[^"]+"' | sort -u | head -20
echo "--- POST /api response ---"
curl -s -X POST http://localhost:5001/api -H 'Content-Type: application/json' -d '{}' | head -c 400
echo ""
echo "--- known endpoint variants ---"
for path in /api /api/suites /tests /test_run; do
  code=$(curl -s -o /dev/null -w "%{http_code}" "http://localhost:5001${path}")
  echo "  ${path}: ${code}"
done
