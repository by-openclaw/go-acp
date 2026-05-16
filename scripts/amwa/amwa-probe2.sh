#!/bin/bash
curl -s http://localhost:5001/ | grep -iE 'form|action|api|test|fetch|/run|input|select' | head -30
