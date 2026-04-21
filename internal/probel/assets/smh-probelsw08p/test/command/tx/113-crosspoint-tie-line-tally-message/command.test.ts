import * as _ from 'lodash';
import { CrossPointTieLineTallyCommand } from '../../../../src/command/tx/113-crosspoint-tie-line-tally-message/command';
import { CrossPointTieLineTallyCommandParams } from '../../../../src/command/tx/113-crosspoint-tie-line-tally-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import { CrossPointTieLineTallyCommandItems } from '../../../../src/command/tx/113-crosspoint-tie-line-tally-message/items';

describe('CrossPoint Tally Dump (Word) Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            // generate an array of CrossPointTieLineTallyCommandItems
            const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
            for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                buildDataArray.push({ sourceMatrixId: 0, sourceLevel: 0, sourceId: itemIndex });
            }

            const params: CrossPointTieLineTallyCommandParams = {
                destinationMatrixId: 0,
                destinationAssociation: 0,
                numberOfSourcesReturned: buildDataArray.length,
                sourceItems: buildDataArray
            };
            // Act
            const command = new CrossPointTieLineTallyCommand(params);

            // Assert
            expect(command).toBeDefined();
            expect(command.params).toBe(params);
            expect(command.identifier).toBe(CommandIdentifiers.TX.GENERAL.CROSSPOINT_TIE_LINE_TALLY_MESSAGE);
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            // generate an array of CrossPointTieLineTallyCommandItems
            const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
            for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                buildDataArray.push({ sourceMatrixId: -1, sourceLevel: -1, sourceId: -1 });
            }

            const params: CrossPointTieLineTallyCommandParams = {
                destinationMatrixId: -1,
                destinationAssociation: -1,
                numberOfSourcesReturned: -1,
                sourceItems: buildDataArray
            };
            // Act
            const fct = () => new CrossPointTieLineTallyCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.destinationMatrixId.id).toBe(
                    CommandErrorsKeys.DESTINATION_MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationAssociation.id).toBe(
                    CommandErrorsKeys.DESTINATION_ASSOCIATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.numberOfSourcesReturned.id).toBe(
                    CommandErrorsKeys.NUMBER_OF_SOURCES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceItems.id).toBe(
                    CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            // generate an array of CrossPointTieLineTallyCommandItems
            const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
            for (let itemIndex = 0; itemIndex < 4; itemIndex++) {
                // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                buildDataArray.push({ sourceMatrixId: 256, sourceLevel: 256, sourceId: 65536 });
            }

            const params: CrossPointTieLineTallyCommandParams = {
                destinationMatrixId: 20,
                destinationAssociation: 65536,
                numberOfSourcesReturned: 257,
                sourceItems: buildDataArray
            };
            // Act
            const fct = () => new CrossPointTieLineTallyCommand(params);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.destinationMatrixId.id).toBe(
                    CommandErrorsKeys.DESTINATION_MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.destinationAssociation.id).toBe(
                    CommandErrorsKeys.DESTINATION_ASSOCIATION_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.numberOfSourcesReturned.id).toBe(
                    CommandErrorsKeys.NUMBER_OF_SOURCES_RETURNED_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceItems.id).toBe(
                    CommandErrorsKeys.SOURCE_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General CrossPoint Tie Line Tally Message CMD_113_0X71', () => {
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.CROSSPOINT_TIE_LINE_TALLY_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                // generate an array of CrossPointTieLineTallyCommandItems
                const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
                for (let itemIndex = 0; itemIndex < 1; itemIndex++) {
                    // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                    // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                    buildDataArray.push({ sourceMatrixId: 0, sourceLevel: 0, sourceId: itemIndex });
                }

                const params: CrossPointTieLineTallyCommandParams = {
                    destinationMatrixId: 0,
                    destinationAssociation: 0,
                    numberOfSourcesReturned: buildDataArray.length,
                    sourceItems: buildDataArray
                };

                // Act
                const command = new CrossPointTieLineTallyCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier,
                    '71 00 00 00 01 00 00 00 00',
                    0x09,
                    0x85,
                    '10 02 71 00 00 00 01 00 00 00 00 09 85 10 03'
                );
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.CROSSPOINT_TIE_LINE_TALLY_MESSAGE;
            });

            it('Should create & pack the extended command (...)', () => {
                // Arrange
                // generate an array of CrossPointTieLineTallyCommandItems
                const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
                for (let itemIndex = 0; itemIndex < 16; itemIndex++) {
                    // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                    // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                    buildDataArray.push({ sourceMatrixId: 16, sourceLevel: 16, sourceId: itemIndex });
                }

                const params: CrossPointTieLineTallyCommandParams = {
                    destinationMatrixId: 16,
                    destinationAssociation: 16,
                    numberOfSourcesReturned: buildDataArray.length,
                    sourceItems: buildDataArray
                };

                // Act
                const command = new CrossPointTieLineTallyCommand(params);
                command.buildCommand();

                // Assert
                Fixture.assertCommand(
                    command,
                    commandIdentifier,
                    '71 10 00 10 10 10 10 00 00 10 10 00 01 10 10 00 02 10 10 00 03 10 10 00 04 10 10 00 05 10 10 00 06 10 10 00 07 10 10 00 08 10 10 00 09 10 10 00 0a 10 10 00 0b 10 10 00 0c 10 10 00 0d 10 10 00 0e 10 10 00 0f',
                    0x45,
                    0xa2,
                    '10 02 71 10 10 00 10 10 10 10 10 10 10 10 00 00 10 10 10 10 00 01 10 10 10 10 00 02 10 10 10 10 00 03 10 10 10 10 00 04 10 10 10 10 00 05 10 10 10 10 00 06 10 10 10 10 00 07 10 10 10 10 00 08 10 10 10 10 00 09 10 10 10 10 00 0a 10 10 10 10 00 0b 10 10 10 10 00 0c 10 10 10 10 00 0d 10 10 10 10 00 0e 10 10 10 10 00 0f 45 a2 10 03'
                );
            });
        });
    });

    describe('to Log Description of the general command', () => {
        it('Should log General command description', () => {
            // Arrange
                // generate an array of CrossPointTieLineTallyCommandItems
                const buildDataArray = new Array<CrossPointTieLineTallyCommandItems>();
                for (let itemIndex = 0; itemIndex < 16; itemIndex++) {
                    // Add the CrossPoint Tie Line Tally Command Source Items buffer to the array
                    // {sourceMatrixId: value, sourceLevel: value, sourceId: value}
                    buildDataArray.push({ sourceMatrixId: 16, sourceLevel: 16, sourceId: itemIndex });
                }

                const params: CrossPointTieLineTallyCommandParams = {
                    destinationMatrixId: 16,
                    destinationAssociation: 16,
                    numberOfSourcesReturned: buildDataArray.length,
                    sourceItems: buildDataArray
                };
            // Act
            const command = new CrossPointTieLineTallyCommand(params);
            const description = command.toLogDescription();

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
