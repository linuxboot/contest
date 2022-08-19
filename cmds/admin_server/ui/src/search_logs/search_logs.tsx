import React, { useState, useEffect, useMemo } from 'react';
import { useToaster, Message } from 'rsuite';
import { TypeAttributes } from 'rsuite/esm/@types/common';
import { FieldMetaData, getLogDescription } from '../api/logs';
import LogTable from './log_table/log_table';
import { renderFilter } from './render_filters';
import './search_logs.scss';

export default function SearchLogs() {
    const [filters, setFilters] = useState<any>({});
    const [description, setDescription] = useState<FieldMetaData[]>();

    const toaster = useToaster();

    const showMsg = (type: TypeAttributes.Status, message: string) => {
        toaster.push(
            <Message showIcon type={type}>
                {message}
            </Message>,
            { placement: 'topEnd' }
        );
    };

    useEffect(() => {
        (async () => {
            try {
                let res = await getLogDescription();
                setDescription(res);
            } catch (err) {
                console.log(err);
                showMsg('error', err?.message);
            }
        })();
    }, []);

    return (
        <div className="logs-search">
            {description
                ?.sort((a, b) => (a.name > b.name ? 1 : -1))
                .map((d, idx) => renderFilter(d, filters, setFilters, idx))}
            <LogTable columns={description} filters={filters} />
        </div>
    );
}
