import _ from 'lodash';
import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { MetaCommandBase } from '../../meta-command.base';
import { CommandParamsUtility, CrossPointTallyDumpWordCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Tally Dump (Word) Message command
 *
 * This message is issued by a controller in response to a CROSSPOINT TALLY DUMP REQUEST (Command Byte 21).
 * It provides tally table data for a given matrix/level combination.
 * This message assumes a maximum destination/source number of 65535.
 *
 * Command issued by Pro-Bel Controller
 *
 * @export
 * @class CrossPointTallyDumpWordCommand
 * @extends {MetaCommandBase<CrossPointTallyDumpWordCommandParams>}
 */
export class CrossPointTallyDumpWordCommand extends MetaCommandBase<CrossPointTallyDumpWordCommandParams, any> {
    /**
     * Creates an instance of CrossPointTallyDumpWordCommand.
     * @param {CrossPointTallyDumpWordCommandParams} params the command parameters
     * @memberof CrossPointTallyDumpWordCommand
     */
    constructor(params: CrossPointTallyDumpWordCommandParams) {
        super(CommandIdentifiers.TX.GENERAL.CROSSPOINT_TALLY_DUMP_DWORD_MESSAGE, params);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof CrossPointTallyDumpWordCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}`;

        return descriptionFor('General');
    }

    /**
     * Build the Pro-Bel SW-P-8 - CrossPoint Tally Dump (Word) Message
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointTallyDumpWordCommand
     */
    protected buildData(): Buffer[] {
        if (this.params.matrixId > 15 || this.params.levelId > 15 || this.params.firstDestinationId > 895) {
            // Creates an array of sourceIdItems split into groups the length of the numberOfTalliesReturned.
            const sourceNumberTallyDumpDataItemsChunck = _.chunk(
                this.params.sourceIdItems,
                this.params.numberOfTalliesReturned
            );

            // Get the first time the First Destination number
            let destinationOffsetId = this.params.firstDestinationId;

            // Gets the list of command to send
            const buildDataArray = new Array<Buffer>();

            // Generates the List of Extended Command to send
            for (let nbrCmdToSend = 0; nbrCmdToSend < sourceNumberTallyDumpDataItemsChunck.length; nbrCmdToSend++) {
                // Add the command buffer to the array
                buildDataArray.push(
                    this.buildDataExtended(
                        this.params,
                        sourceNumberTallyDumpDataItemsChunck[nbrCmdToSend],
                        destinationOffsetId
                    )
                );
                // Add the new First Destination Number update for next command
                destinationOffsetId += sourceNumberTallyDumpDataItemsChunck[nbrCmdToSend].length;
            }
            return buildDataArray;
        } else {
            if (this.params.firstDestinationId + this.params.sourceIdItems.length < 896) {
                // Creates an array of sourceIdItems split into groups the length of the numberOfTalliesReturned.
                const sourceNumberTallyDumpDataItemsChunck = _.chunk(
                    this.params.sourceIdItems,
                    this.params.numberOfTalliesReturned
                );

                // Gets the first time the First destination number
                let destinationOffsetId = this.params.firstDestinationId;

                // Gets the list of commands to send
                const buildDataArray = new Array<Buffer>();

                // Generates the List of General Command to send
                for (let nbrCmdToSend = 0; nbrCmdToSend < sourceNumberTallyDumpDataItemsChunck.length; nbrCmdToSend++) {
                    // Add the command buffer to the array
                    buildDataArray.push(
                        this.buildDataNormal(
                            this.params,
                            sourceNumberTallyDumpDataItemsChunck[nbrCmdToSend],
                            destinationOffsetId
                        )
                    );
                    // Add the new First Destination Number update for next command
                    destinationOffsetId += sourceNumberTallyDumpDataItemsChunck[nbrCmdToSend].length;
                }
                return buildDataArray;
            } else {
                // Creates an array of sourceIdItems split into groups the length of the (896-firstDestinationId).
                const calculateMaxItemsisNormal = 896 - this.params.firstDestinationId;
                const sourceIdItemsNormalSliced = _.slice(this.params.sourceIdItems, 0, calculateMaxItemsisNormal);
                const sourceIdItemsExtendedSliced = _.slice(
                    this.params.sourceIdItems,
                    calculateMaxItemsisNormal,
                    this.params.sourceIdItems.length
                );

                // Creates an array of sourceIdItemsNormalSliced split into groups the length of the numberOfTalliesReturned.
                const sourceIdItemsNormalChunck = _.chunk(
                    sourceIdItemsNormalSliced,
                    this.params.numberOfTalliesReturned
                );

                // Gets the first time the First Destination number
                let destinationOffsetId = this.params.firstDestinationId;

                // Gets the list of command to send
                const buildDataArray = new Array<Buffer>();

                // Generates the List of General Command to send
                for (let nbrCmdToSend = 0; nbrCmdToSend < sourceIdItemsNormalChunck.length; nbrCmdToSend++) {
                    // Add the command buffer to the array
                    buildDataArray.push(
                        this.buildDataNormal(this.params, sourceIdItemsNormalChunck[nbrCmdToSend], destinationOffsetId)
                    );
                    // Add the new First Destination Number update for next command
                    destinationOffsetId += sourceIdItemsNormalChunck[nbrCmdToSend].length;
                }

                // Creates an array of sourceIdItemsExtendedChunck split into groups the length of the numberOfTalliesReturned.
                const sourceIdItemsExtendedChunck = _.chunk(
                    sourceIdItemsExtendedSliced,
                    this.params.numberOfTalliesReturned
                );

                // Gets the first time the First name number
                destinationOffsetId = 896;

                // Generates the List of Extended Command to send
                for (let nbrCmdToSend = 0; nbrCmdToSend < sourceIdItemsExtendedChunck.length; nbrCmdToSend++) {
                    // Add the command buffer to the array
                    buildDataArray.push(
                        this.buildDataExtended(
                            this.params,
                            sourceIdItemsExtendedChunck[nbrCmdToSend],
                            destinationOffsetId
                        )
                    );
                    // Add the new First Destination Number update for next command
                    destinationOffsetId += sourceIdItemsExtendedChunck[nbrCmdToSend].length;
                }
                return buildDataArray;
            }
        }
    }

    /**
     * Validates the command parameters and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {CrossPointTallyDumpWordCommandParams} params the command parameters
     * @memberof CrossPointTallyDumpWordCommand
     */

    private validateParams(params: CrossPointTallyDumpWordCommandParams): void {
        const validationErrors: Record<string, LocaleData> = {
            ...new CommandParamsValidator(params).validate()
        };

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }

    /**
     * Builds general command
     * Returns Probel SW-P-08 - General CrossPoint Tally Dump (Word) Message CMD_023_0X17
     *
     * + This message is issued by a controller in response to a CROSSPOINT TALLY DUMP REQUEST (Command Byte 21). It provides tally table data for a given matrix/level combination.
     * + This message assumes a maximum destination/source number of 65535.
     *
     * + N.B. : The message length is limited in total to 133 bytes, thus, tables that would exceed this length would cause more than one message to be issued with the appropriate destination value in the bytes 3 and 4 of the message.
     *
     * + This message assumes a maximum destination/source number of 65535.
     *
     * | Message                        | Command Byte  | 023 - 0x17                                                                                                   |
     * |--------------------------------|---------------|--------------------------------------------------------------------------------------------------------------|
     * | Byte                           | Field Format  | Notes                                                                                                        |
     * | Byte 1                         | Matrix/Level  | Matrix/Level number as defined in 3.1.2                                                                      |
     * | Byte 2                         | Tallies       | Number of tallies returned (Max 64)                                                                          |
     * | Byte 3                         | 1st Dest mult | First destination number multiplier DIV 256                                                                  |
     * | Byte 4                         | 1st Dest num  | First destination number MOD 256                                                                             |
     * | Byte 5                         | 1st  Src mult | First source number multiplier DIV 256                                                                       |
     * | Byte 6                         | 1st Src num   | First source number MOD 256                                                                                  |
     * | Byte 7                         | 2nd Src mult  | Second source number DIV 256                                                                                 |
     * | Byte 8                         | 2nd Src num   | Second source number MOD 256                                                                                 |
     * |                                |               | Etc                                                                                                          |
     * @private
     * @param {CrossPointTallyDumpWordCommandParams} params
     * @param {number[]} items
     * @param {number} destinationOffsetId
     * @returns {Buffer} the command message
     * @memberof CrossPointTallyDumpWordCommand
     */
    private buildDataNormal(
        params: CrossPointTallyDumpWordCommandParams,
        items: number[],
        destinationOffsetId: number
    ): Buffer {
        const calculateBufferSize = (): number =>
            //
            (items.length - 1) * 2 + 7;
        const buffer = new SmartBuffer({ size: calculateBufferSize() })
            .writeUInt8(CommandIdentifiers.TX.GENERAL.CROSSPOINT_TALLY_DUMP_DWORD_MESSAGE.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(params.matrixId, params.levelId))
            .writeUInt8(params.numberOfTalliesReturned)
            .writeUInt8(destinationOffsetId / 256)
            .writeUInt8(destinationOffsetId % 256);

        for (let itemIndex = 0; itemIndex < items.length; itemIndex++) {
            const data = items[itemIndex];
            buffer.writeUInt8(Math.floor(data / 256)).writeUInt8(data % 256);
        }
        // Add the command buffer to the array
        return buffer.toBuffer();
    }

    /**
     * Builds extended command
     * Returns Probel SW-P-08 - Extended General CrossPoint Tally Dump (Word) Message CMD_151_0X97
     *
     * + This message is issued by a controller in response to an EXTENDED CROSSPOINT TALLY DUMP REQUEST (Command Byte 149). It provides tally table data for a given matrix/level combination.
     * + This message assumes a maximum destination/source number of 65535.
     *
     * + Note: The message length is limited in total to 133 bytes, thus, tables that would exceed this length would cause more than one message to be issued with the appropriate destination value in the bytes 4 and 5 of the message.
     *
     * + This message assumes a maximum destination/source number of 65535.
     *
     * | Message        | Command Byte   | 151-0x97                                                                                                                          |
     * |----------------|----------------|-----------------------------------------------------------------------------------------------------------------------------------|
     * | Byte           | Field Format   | Notes                                                                                                                             |
     * | Byte 1         | Matrix number  |                                                                                                                                   |
     * | Byte 2         | Level number   |                                                                                                                                   |
     * | Byte 3         | Num of Tallies | Number of tallies returned (Maximum 64)                                                                                           |
     * | Byte 4         | 1st Dest mult  | First Destination number multiplier DIV 256                                                                                       |
     * | Byte 5         | 1st Dest num   | First Destination number MOD 256                                                                                                  |
     * | Byte 6         | 1st Src mult   | First Source number multiplier DIV 256                                                                                            |
     * | Byte 7         | 1st Src num    | First Source number MOD 256                                                                                                       |
     * | Byte 8         | 2nd Src mult   | Second source number DIV 256                                                                                                      |
     * | Byte 9         | 2nd Src num    | Second source number MOD 256                                                                                                      |
     * | Etc ...        |                |                                                                                                                                   |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointTallyDumpWordCommand
     */

    private buildDataExtended(
        params: CrossPointTallyDumpWordCommandParams,
        items: number[],
        destinationOffsetId: number
    ): Buffer {
        const calculateBufferSize = (): number =>
            //
            (items.length - 1) * 2 + 8;
        const buffer = new SmartBuffer({ size: calculateBufferSize() })
            .writeUInt8(CommandIdentifiers.TX.EXTENDED.CROSSPOINT_TALLY_DUMP_DWORD_MESSAGE.id)
            .writeUInt8(params.matrixId)
            .writeUInt8(params.levelId)
            .writeUInt8(params.numberOfTalliesReturned)
            .writeUInt8(destinationOffsetId / 256)
            .writeUInt8(destinationOffsetId % 256);

        for (let itemIndex = 0; itemIndex < items.length; itemIndex++) {
            const data = items[itemIndex];
            buffer.writeUInt8(Math.floor(data / 256)).writeUInt8(data % 256);
        }
        // Add the command buffer to the array
        return buffer.toBuffer();
    }
}
