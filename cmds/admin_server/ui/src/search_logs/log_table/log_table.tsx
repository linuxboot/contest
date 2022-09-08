import React, { useState } from 'react';
import { Table, Pagination, Button, useToaster, Message } from 'rsuite';
import { Column, Cell, HeaderCell } from 'rsuite-table';
import { StandardProps } from 'rsuite-table/lib/@types/common';
import { getLogs, Log, Result } from '../../api/logs';
import { TypeAttributes } from 'rsuite/esm/@types/common';
import DateCell from '../../date_cell/date_cell';
import 'rsuite/dist/rsuite.min.css';
import './log_table.scss';

export interface LogTableProps extends StandardProps {
    logLevels?: string;
    queryText?: string;
    jobID?: number;
    startDate?: Date;
    endDate?: Date;
}

export default function LogTable({
    logLevels,
    queryText,
    jobID,
    startDate,
    endDate,
}: LogTableProps) {
    const [loading, setLoading] = useState<boolean>(false);
    const [logs, setLogs] = useState<Log[] | null>([]);
    const [count, setCount] = useState<number>(0);
    const [page, setPage] = useState<number>(0);
    const [limit, setLimit] = useState<number>(20);

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
        setLoading(true);
        try {
            let result: Result = await getLogs({
                job_id: jobID ?? undefined,
                text: queryText,
                page: page,
                page_size: limit,
                log_level: logLevels,
                start_date: startDate?.toJSON(),
                end_date: endDate?.toJSON(),
            });

            setLogs(result.logs);
            setCount(result.count);
            setPage(result.page);
            setLimit(result.page_size);
        } catch (err) {
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
                <Column width={80} align="center" fixed>
                    <HeaderCell>JobID</HeaderCell>
                    <Cell className="log-table__cell" dataKey="job_id" />
                </Column>
                <Column width={250} align="center" fixed>
                    <HeaderCell>Date</HeaderCell>
                    <DateCell className="log-table__cell" dataKey="date" />
                </Column>
                <Column width={80} align="center" fixed>
                    <HeaderCell>Level</HeaderCell>
                    <Cell className="log-table__cell" dataKey="log_level" />
                </Column>
                <Column width={600} align="left" flexGrow={1}>
                    <HeaderCell>Data</HeaderCell>
                    <Cell className="log-table__cell" dataKey="log_data" />
                </Column>
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
