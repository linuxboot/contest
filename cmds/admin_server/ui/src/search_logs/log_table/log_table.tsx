import React, { useMemo, useState } from 'react';
import { Table, Pagination, Button, useToaster, Message } from 'rsuite';
import { StandardProps } from 'rsuite-table/lib/@types/common';
import { TypeAttributes } from 'rsuite/esm/@types/common';
import { renderColumn } from './render_columns';
import {
    getLogs,
    getLogDescription,
    Result,
    FieldMetaData,
} from '../../api/logs';
import 'rsuite/dist/rsuite.min.css';
import './log_table.scss';

export interface LogTableProps extends StandardProps {
    columns?: FieldMetaData[];
    filters?: any;
}

export default function LogTable({ columns, filters }: LogTableProps) {
    const [loading, setLoading] = useState<boolean>(false);
    const [logs, setLogs] = useState<any[] | null>([]);
    const [count, setCount] = useState<number>(0);
    const [page, setPage] = useState<number>(0);
    const [limit, setLimit] = useState<number>(20);

    const renderedColumns = useMemo(
        () =>
            columns
                ?.sort((a, b) => (a.type.length > b.type.length ? 1 : -1))
                .map((c, idx) => renderColumn(c, idx)),
        [columns]
    );

    const toaster = useToaster();
    const pageSizes = [20, 50, 100];

    const showMsg = (type: TypeAttributes.Status, message: string) => {
        toaster.push(
            <Message showIcon type={type}>
                {message}
            </Message>,
            { placement: 'topEnd' }
        );
    };
    const updateLogsTable = async (page: number, limit: number) => {
        getLogDescription();

        setLoading(true);
        try {
            let result: Result = await getLogs({
                page: page,
                page_size: limit,
                ...filters,
            });

            setLogs(result.logs);
            setCount(result.count);
            setPage(result.page);
            setLimit(result.page_size);
        } catch (err) {
            console.log(err);
            showMsg('error', err?.message);
        }
        setLoading(false);
    };

    return (
        <div className="log-table">
            <div>
                <Button
                    className="log-table__search-btn"
                    color="green"
                    appearance="primary"
                    onClick={() => updateLogsTable(0, limit)}
                >
                    Search
                </Button>
            </div>
            <Table
                loading={loading}
                height={700}
                data={logs ?? []}
                wordWrap="break-word"
                rowHeight={30}
            >
                {renderedColumns}
            </Table>
            <div>
                <Pagination
                    prev
                    next
                    first
                    last
                    ellipsis
                    boundaryLinks
                    className="log-table__pagination"
                    maxButtons={5}
                    size="xs"
                    layout={['total', '-', 'limit', '|', 'pager', 'skip']}
                    total={count}
                    limitOptions={pageSizes}
                    limit={limit}
                    activePage={page + 1}
                    onChangePage={(page) => updateLogsTable(page - 1, limit)}
                    onChangeLimit={(limit) => updateLogsTable(0, limit)}
                />
            </div>
        </div>
    );
}
