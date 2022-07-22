import React, { useState, useEffect, useMemo, useRef } from 'react';
import { Input, DateRangePicker, TagPicker, InputNumber } from 'rsuite';
import LogTable from './log_table/log_table';
import { Levels } from '../api/logs';
import './search_logs.scss';

export default function SearchLogs() {
    const [queryText, setQueryText] = useState<string>('');
    const [jobID, setJobID] = useState<number | null>(null);
    const [logLevels, setLogLevels] = useState<string[]>([]);
    const [dateRange, setDateRange] = useState<[Date, Date] | null>(null);

    const levelTags = useMemo(
        () => Levels.map((l) => ({ key: l, label: l })),
        Levels
    );

    return (
        <div className="logs-search">
            <div className="logs-search__input-group">
                <p> Search: </p>
                <Input
                    className="filter-input"
                    placeholder="log data"
                    value={queryText}
                    onChange={setQueryText}
                />
            </div>
            <div className="logs-search__input-group">
                <p> Job ID: </p>
                <InputNumber
                    className="filter-input"
                    placeholder="Job ID"
                    min={0}
                    value={jobID ?? ''}
                    onChange={(id) =>
                        setJobID(id === '' ? null : parseInt(String(id)))
                    }
                />
            </div>
            <div className="logs-search__input-group">
                <p>Date Range:</p>
                <DateRangePicker
                    className="filter-input"
                    format="yyyy-MM-dd HH:mm:ss"
                    value={dateRange}
                    onChange={setDateRange}
                />
            </div>
            <div className="logs-search__input-group">
                <p>Levels:</p>
                <TagPicker
                    className="filter-input"
                    data={levelTags}
                    labelKey="label"
                    valueKey="key"
                    value={logLevels}
                    onChange={setLogLevels}
                    cleanable={false}
                />
            </div>
            <LogTable
                queryText={queryText}
                jobID={jobID ?? undefined}
                logLevels={
                    logLevels.length > 0 ? logLevels.join(',') : undefined
                }
                startDate={dateRange ? dateRange[0] : undefined}
                endDate={dateRange ? dateRange[1] : undefined}
            />
        </div>
    );
}
