import _ from 'lodash';
import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { MetaCommandBase } from '../../meta-command.base';
import { CommandParamsUtility, CrossPointTallyDumpByteCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Tally Dump (Byte) Message command
 *
 * + This message is issued by a controller in response to a CROSSPOINT TALLY DUMP REQUEST (Command Byte 21).
 * + It provides tally table data for a given matrix/level combination.
 *
// TODO: number of Tallies Returned could be more "This message assumes a maximum destination/source number of 191".
 * + number of Tallies Returned min = 1 and max = 64
 * - This message assumes a maximum destination/source number of 191.
 * - This message assumes a maximum destination/source number of 65535.
 *
 * Command issued by Pro-Bel Controller
 * @export
 * @class CrossPointTallyDumpByteCommand
 * @extends {CommandBase<CrossPointTallyDumpByteCommandParams>}
 */
export class CrossPointTallyDumpByteCommand extends MetaCommandBase<CrossPointTallyDumpByteCommandParams, any> {
    /**
     * Creates an instance of CrossPointTallyDumpByteCommand.
     * @param {CrossPointTallyDumpByteCommandParams} params the command parameters
     * @memberof CrossPointTallyDumpByteCommand
     */
    constructor(params: CrossPointTallyDumpByteCommandParams) {
        super(CommandIdentifiers.TX.GENERAL.CROSSPOINT_TALLY_DUMP_BYTE_MESSAGE, params);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof CrossPointTallyDumpByteCommand
     */
    toLogDescription(): string {
        return `General  -   ${this.name}: ${CommandParamsUtility.toString(this.params)}`;
    }

    /**
     * Builds the commands
     *
     * @protected
     * @returns {Buffer[]} the command message
     * @memberof CrossPointTallyDumpByteCommand
     */
    protected buildData(): Buffer[] {
        // Creates an array of sourceIdItems split into groups the length of the (896-firstDestinationId).
        const calculateMaxItemsisNormal = 256 - this.params.firstDestinationId;
        const sourceIdItemsNormalSliced = _.slice(this.params.sourceIdItems, 0, calculateMaxItemsisNormal);

        // Creates an array of sourceIdItemsNormalSliced split into groups the length of the numberOfTalliesReturned.
        const sourceIdItemsNormalChunck = _.chunk(sourceIdItemsNormalSliced, this.params.numberOfTalliesReturned);

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

        return buildDataArray;
    }

    /**
     * Builds general command
     * Returns Probel SW-P-08 - General CrossPoint Tally Dump (byte) Message CMD_022_0X16
     *
     * + This message is issued by a controller in response to a CROSSPOINT TALLY DUMP REQUEST (Command Byte 21).
     * + It provides tally table data for a given matrix/level combination.
     * + This message assumes a maximum destination/source number of 191.
     *
     * | Message                        | Command Byte  | 022 - 0x16                                                                                                   |
     * |--------------------------------|---------------|--------------------------------------------------------------------------------------------------------------|
     * | Byte                           | Field Format  | Notes                                                                                                        |
     * | Byte 1                         | Matrix/Level  | Matrix/Level number as defined in 3.1.2                                                                      |
     * | Byte 2                         | Tallies       | Number of tallies returned (Max 191)  = sourceIdItems.length                                                 |
     * | Byte 3                         | 1st Dest num  | First destination number                                                                                     |
     * | Byte 4                         | 1st Src num   | First source number                                                                                          |
     * | Byte 5                         | 2nd Src num   | Second source number                                                                                         |
     * |                                |               | Etc                                                                                                          |
     *
     * @private
     * @param {CrossPointTallyDumpByteCommandParams} params
     * @param {number[]} items
     * @param {number} destinationOffsetId
     * @returns {Buffer} the command message
     * @memberof CrossPointTallyDumpByteCommand
     */
    private buildDataNormal(
        params: CrossPointTallyDumpByteCommandParams,
        items: number[],
        destinationOffsetId: number
    ): Buffer {
        const calculateBufferSize = (): number =>
            //
            items.length + 4;
        const buffer = new SmartBuffer({ size: calculateBufferSize() })
            .writeUInt8(CommandIdentifiers.TX.GENERAL.CROSSPOINT_TALLY_DUMP_BYTE_MESSAGE.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(params.matrixId, params.levelId))
            .writeUInt8(params.numberOfTalliesReturned)
            .writeUInt8(destinationOffsetId);

        for (let itemIndex = 0; itemIndex < items.length; itemIndex++) {
            const data = items[itemIndex];
            buffer.writeUInt8(data);
        }
        // Add the command buffer to the array
        return buffer.toBuffer();
    }

    /**
     * Validate the command parameters and throw a ValidationError in case of error
     * @private
     * @param {CrossPointTallyDumpByteCommandParams} params the command parameters
     * @memberof CrossPointTallyDumpByteCommand
     */
    private validateParams(params: CrossPointTallyDumpByteCommandParams): void {
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
}
