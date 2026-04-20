import { CommandBase } from '../command.base';

export class BufferredCommand extends CommandBase<any, any> {
    constructor() {
        super(
            {
                id: 0,
                name: '',
                rxTxType: 'RX',
                isExtended: false
            },
            {}
        );
    }

    toLogDescription(): string {
        throw new Error('Method not implemented.');
    }

    decode(buffer: Buffer): void {
        /**
        const __buffer: Buffer = buffer;
        const  startOfMessage: SOM = buffer.slice(0, 2);
        const data: DATA = buffer.slice(2, );
        const bytesCount: BTC = ;
        const checksum: CHK = ;
        const endOfMessage: EOM = ;
        */
    }
    // @TODO: should be only part of UserCommand
    protected buildData(): Buffer {
        throw new Error('Method not implemented.');
    }
}
