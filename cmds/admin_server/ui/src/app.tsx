import React from 'react';
import { Routes, Route, Link } from 'react-router-dom';
import SearchTags from './search_tags/search_tags';
import SearchLogs from './search_logs/search_logs';
import Jobs from './jobs/jobs';
import './app.scss';

export default function App() {
    return (
        <>
            <div className="navbar">
                <div className="navbar__logo">ConTest</div>
                <div className="navbar__btns">
                    <Link
                        className="navbar__link"
                        to="/app"
                        title="search logs"
                    >
                        Logs
                    </Link>
                    <Link
                        className="navbar__link"
                        to="/app/tag"
                        title="search tags"
                    >
                        Tags
                    </Link>
                </div>
            </div>
            <Routes>
                <Route path="/app">
                    <Route index element={<SearchLogs />} />
                    <Route path="tag">
                        <Route index element={<SearchTags />} />
                        <Route path=":name" element={<Jobs />} />
                    </Route>
                </Route>
            </Routes>
        </>
    );
}
