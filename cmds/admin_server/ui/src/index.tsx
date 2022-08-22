import React from 'react';
import { BrowserRouter as Router } from 'react-router-dom';
import ReactDOM from 'react-dom';
import App from './app';

const el = document.getElementById('app');

ReactDOM.render(
    <Router>
        <App />
    </Router>,
    el
);
