import React, { useState } from 'react';
import { Link } from 'react-router-dom';
import { Button, useToaster, Message } from 'rsuite';
import { StandardProps } from 'rsuite-table/lib/@types/common';
import { TypeAttributes } from 'rsuite/esm/@types/common';
import { Tag, getTags } from '../../api/tags';
import './tags_grid.scss';

export interface TagsGridProps extends StandardProps {
    tagPattern: string;
}

export default function TagsGrid(props: TagsGridProps) {
    const [tags, setTags] = useState<Tag[]>([]);
    const toaster = useToaster();

    const showMsg = (type: TypeAttributes.Status, message: string) => {
        toaster.push(
            <Message showIcon type={type}>
                {message}
            </Message>,
            { placement: 'topEnd' }
        );
    };

    const updateGrid = async () => {
        try {
            let result: Tag[] = await getTags({
                text: props.tagPattern,
            });

            setTags(result || []);
        } catch (err) {
            showMsg('error', err?.message);
        }
    };

    return (
        <div className="tag-grid">
            <div>
                <Button
                    className="tag-grid__search-btn"
                    color="green"
                    appearance="primary"
                    disabled={props.tagPattern === ''}
                    onClick={updateGrid}
                >
                    Search
                </Button>
            </div>
            <div className="tags">
                {tags.map((t, key) => (
                    <div key={key}>
                        <p>
                            <strong>Tag Name:</strong>
                            <Link to={`/app/tag/${t.name}`}>{t.name}</Link>
                        </p>
                        <p>
                            <strong>Number of Jobs:</strong>
                            {t.jobs_count}
                        </p>
                    </div>
                ))}
            </div>
        </div>
    );
}
